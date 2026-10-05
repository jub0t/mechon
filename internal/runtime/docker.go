//go:build linux

package runtime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// Docker implements Runtime on the Docker Engine API.
type Docker struct {
	cfg     Config
	api     *dockerClient
	dataDir string
	log     *slog.Logger
	http    *http.Client

	runsc atomic.Bool

	mu       sync.Mutex
	expected map[string]time.Time // container IDs the runtime stopped/removed on purpose
	oomed    map[string]bool      // container IDs that saw an oom event
	live     map[string]*liveInfo // container ID → cached pid/cgroup for stats
	cpuPrev  map[string]cpuSample // container ID → previous CPU sample
}

// NewDocker connects to Docker and prepares the node: data directories, the bot network, the
// egress firewall, and mounts of existing volumes.
func NewDocker(ctx context.Context, cfg Config) (*Docker, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "/var/lib/mechon"
	}
	if cfg.Socket == "" {
		cfg.Socket = "/var/run/docker.sock"
	}
	if cfg.DNS == nil {
		cfg.DNS = DefaultDNS
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		}}
	}
	d := &Docker{
		cfg:      cfg,
		api:      newDockerClient(cfg.Socket),
		dataDir:  cfg.DataDir,
		log:      cfg.Logger,
		http:     cfg.HTTPClient,
		expected: map[string]time.Time{},
		oomed:    map[string]bool{},
		live:     map[string]*liveInfo{},
		cpuPrev:  map[string]cpuSample{},
	}
	info, err := d.api.info(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	_, runsc := info.Runtimes["runsc"]
	d.runsc.Store(runsc)

	for _, dir := range []string{"", "volumes", "mounts", "tmp"} {
		p := filepath.Join(d.dataDir, dir)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return nil, err
		}
		if err := os.Chmod(p, 0o700); err != nil {
			return nil, err
		}
	}
	if err := d.ensureNetwork(ctx); err != nil {
		return nil, err
	}
	if err := installFirewall(cfg.DNS); err != nil {
		return nil, fmt.Errorf("firewall: %w", err)
	}
	if err := d.remountAll(); err != nil {
		d.log.Error("remount volumes", "err", err)
	}
	// Install containers never survive an agent restart.
	if cs, err := d.api.listContainers(ctx, labelInstall); err == nil {
		for _, c := range cs {
			_ = d.api.removeContainer(ctx, c.ID)
		}
	}
	return d, nil
}

// EnsureFirewall re-installs the egress rules if something removed them.
func (d *Docker) EnsureFirewall() error {
	if checkFirewall() {
		return nil
	}
	d.log.Warn("egress firewall rules missing, reinstalling")
	return installFirewall(d.cfg.DNS)
}

func (d *Docker) ensureNetwork(ctx context.Context) error {
	n, err := d.api.inspectNetwork(ctx, networkName)
	if err == nil {
		if n.Options["com.docker.network.bridge.enable_icc"] != "false" {
			return fmt.Errorf("network %s exists without enable_icc=false; remove it and restart the agent", networkName)
		}
		return nil
	}
	if !isNotFound(err) {
		return err
	}
	return d.api.createNetwork(ctx, networkName, map[string]string{
		"com.docker.network.bridge.name":                 bridgeName,
		"com.docker.network.bridge.enable_icc":           "false",
		"com.docker.network.bridge.enable_ip_masquerade": "true",
	}, map[string]string{"mechon": "1"})
}

func containerName(botID string) string { return "mechon-" + botID }
func installName(botID string) string   { return "mechon-install-" + botID }

// ---------- Apply ----------

func (d *Docker) Apply(ctx context.Context, spec proto.BotSpec) (Instance, error) {
	if err := ValidateSpec(spec); err != nil {
		return Instance{}, err
	}
	if spec.Hardened && !d.runsc.Load() {
		return Instance{}, errors.New("hardened bot needs the runsc (gVisor) runtime, which this node does not have")
	}
	volErr := d.ensureVolume(spec.BotID, spec.UID, spec.Limits.DiskMB)
	if volErr != nil && !errors.Is(volErr, errShrink) {
		return Instance{}, fmt.Errorf("volume: %w", volErr)
	}
	if volErr != nil {
		d.log.Error("volume", "bot", spec.BotID, "err", volErr)
	}

	current := d.readDeployID(spec.BotID)
	var deployErr error
	deployed := false
	if spec.DeployID != "" && spec.DeployID != current {
		if deployErr = d.deploy(ctx, spec); deployErr == nil {
			current, deployed = spec.DeployID, true
		} else {
			d.emitDeploy(spec, proto.PhaseFailed, nil, deployErr.Error())
		}
	}
	inst, err := d.converge(ctx, spec, current)
	if deployed {
		if err != nil {
			d.emitDeploy(spec, proto.PhaseFailed, nil, "deployed, but the container did not start: "+err.Error())
		} else {
			d.emitDeploy(spec, proto.PhaseLive, nil, "")
		}
	}
	return inst, errors.Join(deployErr, err, volErr)
}

// converge makes the bot container match spec with the code of deployID.
func (d *Docker) converge(ctx context.Context, spec proto.BotSpec, deployID string) (Instance, error) {
	inst := Instance{BotID: spec.BotID, DeployID: deployID, HasVolume: true}
	name := containerName(spec.BotID)
	cur, err := d.api.inspectContainer(ctx, name)
	exists := err == nil
	if err != nil && !isNotFound(err) {
		return inst, err
	}
	if deployID == "" {
		// Nothing deployed: there is nothing to run.
		if exists {
			if err := d.destroyContainer(ctx, cur.ID); err != nil {
				return inst, err
			}
		}
		return inst, nil
	}
	cfg := d.botContainerConfig(spec, deployID)
	if exists && cur.Config.Labels[labelSpec] != cfg.Labels[labelSpec] {
		if err := d.destroyContainer(ctx, cur.ID); err != nil {
			return inst, err
		}
		exists = false
	}
	if !exists {
		if err := d.ensureImage(ctx, spec.Template.Image, func(l string) { d.systemLog(spec.BotID, l) }); err != nil {
			return inst, err
		}
		id, err := d.api.createContainer(ctx, name, cfg)
		if err != nil {
			return inst, fmt.Errorf("create container: %w", err)
		}
		if cur, err = d.api.inspectContainer(ctx, id); err != nil {
			return inst, err
		}
	}
	switch {
	case spec.Desired == proto.DesiredRunning && !cur.State.Running:
		d.systemLog(spec.BotID, "starting")
		if err := d.api.startContainer(ctx, cur.ID); err != nil {
			return inst, fmt.Errorf("start: %w", err)
		}
	case spec.Desired == proto.DesiredStopped && cur.State.Running:
		d.systemLog(spec.BotID, "stopping")
		if err := d.stopContainer(ctx, cur.ID); err != nil {
			return inst, err
		}
	}
	return d.instance(ctx, spec.BotID, cur.ID, deployID)
}

func (d *Docker) instance(ctx context.Context, botID, containerID, deployID string) (Instance, error) {
	inst := Instance{BotID: botID, DeployID: deployID, HasVolume: true}
	c, err := d.api.inspectContainer(ctx, containerID)
	if err != nil {
		return inst, err
	}
	inst.ContainerID = c.ID
	inst.Running = c.State.Running
	inst.ExitCode = c.State.ExitCode
	inst.OOMKilled = c.State.OOMKilled
	return inst, nil
}

func (d *Docker) expect(containerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	d.expected[containerID] = now
	for id, t := range d.expected {
		if now.Sub(t) > 10*time.Minute {
			delete(d.expected, id)
		}
	}
}

func (d *Docker) stopContainer(ctx context.Context, id string) error {
	d.expect(id)
	if err := d.api.stopContainer(ctx, id, stopGraceSec); err != nil && !isNotFound(err) {
		return fmt.Errorf("stop: %w", err)
	}
	return nil
}

func (d *Docker) destroyContainer(ctx context.Context, id string) error {
	if err := d.stopContainer(ctx, id); err != nil {
		return err
	}
	if err := d.api.removeContainer(ctx, id); err != nil {
		return fmt.Errorf("remove container: %w", err)
	}
	d.mu.Lock()
	delete(d.live, id)
	delete(d.cpuPrev, id)
	delete(d.oomed, id)
	d.mu.Unlock()
	return nil
}

func (d *Docker) ensureImage(ctx context.Context, ref string, progress func(string)) error {
	ok, err := d.api.imageExists(ctx, ref)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	progress("pulling image " + ref)
	if err := d.api.pullImage(ctx, ref, progress); err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}
	return nil
}

func botEnv(spec proto.BotSpec) []string {
	env := make([]string, 0, len(spec.Env)+2)
	for k, v := range spec.Env {
		if k == "HOME" || k == "DATA_DIR" {
			continue
		}
		env = append(env, k+"="+v)
	}
	slices.Sort(env)
	return append(env, "HOME="+containerHome, "DATA_DIR="+containerData)
}

// limitsHostConfig is shared by the bot and its install container.
func (d *Docker) limitsHostConfig(spec proto.BotSpec) hostConfig {
	pids := int64(spec.Limits.Pids)
	if pids == 0 {
		pids = defaultPids
	}
	mem := int64(spec.Limits.MemoryMB) * mib
	init := true
	hc := hostConfig{
		NetworkMode:   networkName,
		RestartPolicy: restartPolicy{Name: "no"},
		CapDrop:       []string{"ALL"},
		SecurityOpt:   []string{"no-new-privileges:true"},
		Memory:        mem,
		MemorySwap:    mem,
		NanoCpus:      int64(spec.Limits.CPUMillicores) * 1_000_000,
		PidsLimit:     &pids,
		Ulimits:       []ulimit{{Name: "nofile", Soft: 4096, Hard: 4096}},
		LogConfig:     logConfig{Type: "local", Config: map[string]string{"max-size": "10m", "max-file": "3"}},
		Init:          &init, // tini as PID 1: forwards SIGTERM, reaps zombies
		IpcMode:       "private",
		DNS:           d.cfg.DNS,
	}
	if spec.Hardened {
		hc.Runtime = "runsc"
	}
	return hc
}

// botContainerConfig is spec §5's table.
func (d *Docker) botContainerConfig(spec proto.BotSpec, deployID string) containerConfig {
	hc := d.limitsHostConfig(spec)
	hc.ReadonlyRootfs = true
	hc.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=64m"}
	hc.Binds = []string{
		d.appDir(spec.BotID) + ":" + containerHome + ":rw",
		d.dataPath(spec.BotID) + ":" + containerData + ":rw",
	}
	user := fmt.Sprintf("%d:%d", spec.UID, spec.UID)
	cfg := containerConfig{
		Hostname:   hostname(spec.Name),
		User:       user,
		Env:        botEnv(spec),
		Cmd:        []string{"sh", "-c", spec.Template.Start},
		Entrypoint: []string{""},
		Image:      spec.Template.Image,
		WorkingDir: containerHome,
		Labels: map[string]string{
			labelBot:    spec.BotID,
			labelDeploy: deployID,
		},
		HostConfig: hc,
	}
	h := sha256.New()
	_ = json.NewEncoder(h).Encode(struct {
		V   int
		Cfg containerConfig
	}{specVersion, cfg})
	cfg.Labels[labelSpec] = hex.EncodeToString(h.Sum(nil))[:16]
	return cfg
}

// ---------- Remove / Restart / List ----------

func (d *Docker) Remove(ctx context.Context, botID string) error {
	if !validBotID(botID) {
		return fmt.Errorf("invalid bot id %q", botID)
	}
	for _, name := range []string{installName(botID), containerName(botID)} {
		c, err := d.api.inspectContainer(ctx, name)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := d.destroyContainer(ctx, c.ID); err != nil {
			return err
		}
	}
	if err := d.unmountVolume(botID); err != nil {
		return err
	}
	if err := os.Remove(d.imagePath(botID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(d.mountPath(botID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.RemoveAll(filepath.Join(d.dataDir, "tmp", botID))
	return nil
}

func (d *Docker) Restart(ctx context.Context, botID string) (Instance, error) {
	if !validBotID(botID) {
		return Instance{}, fmt.Errorf("invalid bot id %q", botID)
	}
	c, err := d.api.inspectContainer(ctx, containerName(botID))
	if err != nil {
		if isNotFound(err) {
			return Instance{BotID: botID}, errors.New("bot has no container (nothing deployed)")
		}
		return Instance{}, err
	}
	if c.State.Running {
		d.systemLog(botID, "stopping")
		if err := d.stopContainer(ctx, c.ID); err != nil {
			return Instance{}, err
		}
	}
	d.systemLog(botID, "starting")
	if err := d.api.startContainer(ctx, c.ID); err != nil {
		return Instance{}, fmt.Errorf("start: %w", err)
	}
	return d.instance(ctx, botID, c.ID, c.Config.Labels[labelDeploy])
}

func (d *Docker) List(ctx context.Context) ([]Instance, error) {
	cs, err := d.api.listContainers(ctx, labelBot)
	if err != nil {
		return nil, err
	}
	byID := map[string]*Instance{}
	var order []string
	add := func(id string) *Instance {
		if in, ok := byID[id]; ok {
			return in
		}
		in := &Instance{BotID: id}
		byID[id] = in
		order = append(order, id)
		return in
	}
	for _, c := range cs {
		id := c.Labels[labelBot]
		if !validBotID(id) {
			continue
		}
		in := add(id)
		in.ContainerID = c.ID
		in.Running = c.State == "running"
		if !in.Running {
			if j, err := d.api.inspectContainer(ctx, c.ID); err == nil {
				in.ExitCode, in.OOMKilled = j.State.ExitCode, j.State.OOMKilled
			}
		}
	}
	vols, err := d.volumeIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range vols {
		in := add(id)
		in.HasVolume = true
		in.DeployID = d.readDeployID(id)
	}
	out := make([]Instance, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// ---------- Events ----------

func (d *Docker) Events(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		since := time.Now()
		for ctx.Err() == nil {
			last, err := d.watchEvents(ctx, since, ch)
			if !last.IsZero() {
				since = last.Add(time.Nanosecond)
			}
			if ctx.Err() != nil {
				return
			}
			d.log.Warn("docker events stream ended, reconnecting", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
	return ch, nil
}

func (d *Docker) watchEvents(ctx context.Context, since time.Time, ch chan<- Event) (time.Time, error) {
	body, err := d.api.events(ctx, since, map[string][]string{
		"type":  {"container"},
		"label": {labelBot},
		"event": {"start", "die", "oom"},
	})
	if err != nil {
		return time.Time{}, err
	}
	defer body.Close()
	var last time.Time
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var ev dockerEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		last = time.Unix(0, ev.TimeNano)
		id := ev.Actor.ID
		botID := ev.Actor.Attributes[labelBot]
		switch ev.Action {
		case "oom":
			d.mu.Lock()
			d.oomed[id] = true
			d.mu.Unlock()
			d.systemLog(botID, "out of memory: the kernel killed a process")
		case "start":
			d.mu.Lock()
			delete(d.live, id)
			d.mu.Unlock()
		case "die":
			d.mu.Lock()
			delete(d.live, id)
			_, expected := d.expected[id]
			delete(d.expected, id)
			oom := d.oomed[id]
			delete(d.oomed, id)
			d.mu.Unlock()
			if expected {
				continue
			}
			e := Event{BotID: botID, ContainerID: id, At: last, OOMKilled: oom}
			fmt.Sscan(ev.Actor.Attributes["exitCode"], &e.ExitCode)
			if c, err := d.api.inspectContainer(ctx, id); err == nil {
				e.OOMKilled = e.OOMKilled || c.State.OOMKilled
				e.ExitCode = c.State.ExitCode
			}
			select {
			case ch <- e:
			case <-ctx.Done():
				return last, ctx.Err()
			}
		}
	}
	return last, sc.Err()
}

// ---------- Hooks ----------

func (d *Docker) systemLog(botID, text string) {
	if d.cfg.Hooks.SystemLog != nil && botID != "" {
		d.cfg.Hooks.SystemLog(botID, text)
	}
}

func (d *Docker) emitDeploy(spec proto.BotSpec, phase string, lines []proto.LogLine, errMsg string) {
	if d.cfg.Hooks.DeployProgress != nil {
		d.cfg.Hooks.DeployProgress(proto.DeployProgress{
			DeployID: spec.DeployID, BotID: spec.BotID, Phase: phase, Lines: lines, Error: errMsg,
		})
	}
}
