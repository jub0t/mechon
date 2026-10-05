//go:build linux

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// inspectRaw returns docker inspect's full JSON for checking container settings.
func inspectRaw(t *testing.T, name string) map[string]any {
	t.Helper()
	out, err := exec.Command("docker", "inspect", name).Output()
	if err != nil {
		t.Fatalf("docker inspect %s: %v", name, err)
	}
	var v []map[string]any
	if err := json.Unmarshal(out, &v); err != nil || len(v) != 1 {
		t.Fatalf("inspect json: %v", err)
	}
	return v[0]
}

func TestRuntimeLifecycle(t *testing.T) {
	e := newTestEnv(t)
	d := e.d
	events, err := d.Events(e.ctx)
	if err != nil {
		t.Fatal(err)
	}

	spec := testSpecBase("life-1", 100101)
	spec.Limits.DiskMB = 64

	// No deploy yet: volume prepared, nothing running.
	inst := e.apply(spec)
	if inst.ContainerID != "" {
		t.Fatalf("container without a deploy: %+v", inst)
	}
	if src, _ := mountSource(d.mountPath(spec.BotID)); !strings.HasPrefix(src, "/dev/loop") {
		t.Fatalf("volume not loop-mounted: %q", src)
	}
	for _, sub := range []string{"app", "data"} {
		var st syscall.Stat_t
		if err := syscall.Stat(filepath.Join(d.mountPath(spec.BotID), sub), &st); err != nil {
			t.Fatal(err)
		}
		if st.Uid != 100101 || st.Gid != 100101 {
			t.Fatalf("%s owned by %d:%d", sub, st.Uid, st.Gid)
		}
	}
	if out, _ := sh(t, "findmnt -no OPTIONS "+d.mountPath(spec.BotID)); !strings.Contains(out, "nosuid") || !strings.Contains(out, "nodev") {
		t.Fatalf("volume mount options: %s", out)
	}

	// Deploy.
	spec.Template.Start = "sh run.sh"
	e.withCode(&spec, "dep-1", map[string]string{"run.sh": `echo "hello from $BOT_ID home=$HOME data=$DATA_DIR"; exec sleep 3600`})
	inst = e.apply(spec)
	if !inst.Running || inst.DeployID != "dep-1" {
		t.Fatalf("not running after deploy: %+v", inst)
	}
	if got := e.rec.phases("dep-1"); !slices.Equal(got, []string{proto.PhaseFetching, proto.PhaseInstalling, proto.PhaseLive}) {
		t.Fatalf("phases %v", got)
	}
	firstID := inst.ContainerID

	// Every row of spec §5.
	c := inspectRaw(t, containerName(spec.BotID))
	hc := c["HostConfig"].(map[string]any)
	cfg := c["Config"].(map[string]any)
	check := func(name string, ok bool) {
		t.Helper()
		if !ok {
			b, _ := json.MarshalIndent(hc, "", " ")
			t.Errorf("container setting wrong: %s\n%s", name, b)
		}
	}
	check("user", cfg["User"] == "100101:100101")
	check("readonly rootfs", hc["ReadonlyRootfs"] == true)
	check("tmpfs", hc["Tmpfs"].(map[string]any)["/tmp"] == "rw,noexec,nosuid,nodev,size=64m")
	check("memory", hc["Memory"].(float64) == 128*mib && hc["MemorySwap"].(float64) == 128*mib)
	check("cpu", hc["NanoCpus"].(float64) == 500e6)
	check("pids", hc["PidsLimit"].(float64) == 64)
	check("capdrop", len(hc["CapDrop"].([]any)) == 1 && hc["CapDrop"].([]any)[0] == "ALL" && hc["CapAdd"] == nil)
	check("no-new-privileges", slices.Contains(hc["SecurityOpt"].([]any), any("no-new-privileges:true")))
	check("privileged", hc["Privileged"] == false)
	check("ulimit", strings.Contains(mustJSON(hc["Ulimits"]), `"Hard":4096,"Name":"nofile","Soft":4096`))
	check("log driver", hc["LogConfig"].(map[string]any)["Type"] == "local")
	check("restart policy", hc["RestartPolicy"].(map[string]any)["Name"] == "no")
	check("network", hc["NetworkMode"] == "mechon0")
	check("no ports", len(mapOrNil(hc["PortBindings"])) == 0)
	check("labels", cfg["Labels"].(map[string]any)["mechon.bot"] == spec.BotID && cfg["Labels"].(map[string]any)["mechon.deploy"] == "dep-1")
	check("hostname", cfg["Hostname"] == "test-life-1")
	check("workdir", cfg["WorkingDir"] == "/home/container")
	check("cmd", mustJSON(cfg["Cmd"]) == `["sh","-c","sh run.sh"]`)

	// Logs.
	waitFor(t, "hello in logs", 10*time.Second, func() bool {
		return strings.Contains(logText(t, d, spec.BotID), "hello from life-1 home=/home/container data=/data")
	})

	// Idempotent.
	inst = e.apply(spec)
	if inst.ContainerID != firstID || !inst.Running {
		t.Fatalf("re-apply changed the container: %s -> %s", firstID, inst.ContainerID)
	}

	// Stop, start, restart.
	spec.Desired = proto.DesiredStopped
	start := time.Now()
	if inst = e.apply(spec); inst.Running || inst.ContainerID != firstID {
		t.Fatalf("stop: %+v", inst)
	}
	if time.Since(start) > 8*time.Second {
		t.Errorf("stop took %s; SIGTERM should have ended sleep via tini", time.Since(start))
	}
	spec.Desired = proto.DesiredRunning
	if inst = e.apply(spec); !inst.Running {
		t.Fatalf("start: %+v", inst)
	}
	if inst, err = d.Restart(e.ctx, spec.BotID); err != nil || !inst.Running {
		t.Fatalf("restart: %+v %v", inst, err)
	}

	// None of that is an unexpected exit.
	select {
	case ev := <-events:
		t.Fatalf("unexpected event for a deliberate stop/restart: %+v", ev)
	case <-time.After(time.Second):
	}

	// A crash is reported.
	if out, err := inBot(t, spec.BotID, "echo bye > /data/keep; pkill sleep"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	select {
	case ev := <-events:
		if ev.BotID != spec.BotID || ev.ExitCode == 0 {
			t.Fatalf("crash event: %+v", ev)
		}
		t.Logf("crash event: exit %d", ev.ExitCode)
	case <-time.After(15 * time.Second):
		t.Fatal("no crash event")
	}

	// Limit change: recreated, not redeployed.
	spec.Limits.MemoryMB = 256
	inst = e.apply(spec)
	if inst.ContainerID == firstID || !inst.Running {
		t.Fatalf("limit change did not recreate: %+v", inst)
	}
	if got := e.rec.phases("dep-1"); len(got) != 3 {
		t.Fatalf("limit change redeployed: %v", got)
	}

	// Stats.
	d.Stats(e.ctx)
	time.Sleep(time.Second)
	stats, err := d.Stats(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].MemoryBytes <= 0 || stats[0].DiskBytes <= 0 {
		t.Fatalf("stats: %+v", stats)
	}
	t.Logf("stats: %+v", stats[0])

	// Grow the disk online; data survives.
	spec.Limits.DiskMB = 96
	inst = e.apply(spec)
	if st, _ := os.Stat(d.imagePath(spec.BotID)); st.Size() != 96*mib {
		t.Fatalf("image size %d", st.Size())
	}
	if out, _ := inBot(t, spec.BotID, "df -m /data | tail -1 | awk '{print $2}'; cat /data/keep"); !strings.Contains(out, "bye") {
		t.Fatalf("after grow: %s", out)
	} else {
		t.Logf("after grow, df /data size (MB) and data: %q", out)
	}
	// Shrinking is refused, the bot keeps running.
	spec.Limits.DiskMB = 64
	inst, err = d.Apply(e.ctx, spec)
	if !errors.Is(err, errShrink) || !inst.Running {
		t.Fatalf("shrink: %+v %v", inst, err)
	}
	spec.Limits.DiskMB = 96

	// Agent restart: a new runtime on the same data dir adopts the bot without redeploying.
	d2, err := NewDocker(e.ctx, Config{DataDir: d.dataDir, PanelURL: e.panel.URL, NodeToken: testToken,
		Hooks: e.rec.hooks(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	before := inst.ContainerID
	if inst, err = d2.Apply(e.ctx, spec); err != nil || inst.ContainerID != before || !inst.Running {
		t.Fatalf("adopt after restart: %+v %v", inst, err)
	}
	if got := e.rec.phases("dep-1"); len(got) != 3 {
		t.Fatalf("restart redeployed: %v", got)
	}
	list, err := d2.List(e.ctx)
	if err != nil || len(list) != 1 || list[0].DeployID != "dep-1" || !list[0].Running {
		t.Fatalf("list: %+v %v", list, err)
	}

	// Remove.
	if err := d.Remove(e.ctx, spec.BotID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.api.inspectContainer(e.ctx, containerName(spec.BotID)); !isNotFound(err) {
		t.Fatalf("container still there: %v", err)
	}
	if _, err := os.Stat(d.imagePath(spec.BotID)); !os.IsNotExist(err) {
		t.Fatalf("image still there: %v", err)
	}
	if src, _ := mountSource(d.mountPath(spec.BotID)); src != "" {
		t.Fatalf("still mounted from %s", src)
	}
	if err := d.Remove(e.ctx, spec.BotID); err != nil {
		t.Fatalf("remove is not idempotent: %v", err)
	}
}

func logText(t *testing.T, d *Docker, botID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := d.Logs(ctx, botID, 100, time.Time{}, false)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for l := range ch {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func mapOrNil(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func TestHardenedNeedsRunsc(t *testing.T) {
	e := newTestEnv(t)
	if e.d.runsc.Load() {
		t.Skip("runsc is installed here")
	}
	spec := testSpecBase("hard-1", 100102)
	spec.Hardened = true
	if _, err := e.d.Apply(e.ctx, spec); err == nil || !strings.Contains(err.Error(), "runsc") {
		t.Fatalf("hardened without runsc: %v", err)
	}
}
