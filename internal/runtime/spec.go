package runtime

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// Portable parts of the runtime: configuration, spec validation and shared types. The Docker
// implementation itself is Linux-only (docker.go and friends); see runtime_other.go.

const (
	labelBot     = "mechon.bot"
	labelDeploy  = "mechon.deploy"
	labelSpec    = "mechon.spec"
	labelInstall = "mechon.install"

	stopGraceSec  = 10
	defaultPids   = 128
	minMemoryMB   = 16
	minDiskMB     = 16
	containerHome = "/home/container"
	containerData = "/data"
	// specVersion is folded into the container config hash; bump it when containerConfig
	// changes so existing bots are recreated with the new settings.
	specVersion = 1
)

type Config struct {
	DataDir   string // default /var/lib/mechon
	Socket    string // default /var/run/docker.sock
	PanelURL  string // artifacts are fetched from PanelURL + spec.ArtifactPath
	NodeToken string
	// DNS servers given to bot containers. Docker's embedded resolver forwards to these from
	// inside the bot's network namespace, so they must be reachable through the egress
	// firewall. Default: public resolvers. Private addresses listed here get a port-53 hole.
	DNS    []string
	Hooks  Hooks
	Logger *slog.Logger
	// HTTPClient downloads artifacts. Default: a client with sane timeouts.
	HTTPClient *http.Client
}

var DefaultDNS = []string{"1.1.1.1", "8.8.8.8"}

var botIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func validBotID(id string) bool { return botIDRe.MatchString(id) }

// ValidBotID reports whether id is safe to use as a bot ID (it ends up in paths and names).
func ValidBotID(id string) bool { return validBotID(id) }

// ValidateSpec checks a spec before anything acts on it. The bot ID ends up in file paths and
// container names, the UID in file ownership, so both are checked strictly.
func ValidateSpec(s proto.BotSpec) error {
	switch {
	case !validBotID(s.BotID):
		return fmt.Errorf("invalid bot id %q", s.BotID)
	case s.UID < 1000 || s.UID > 1<<31-2:
		return fmt.Errorf("invalid uid %d (must be >= 1000)", s.UID)
	case s.Template.Image == "" || strings.ContainsAny(s.Template.Image, " \t\n?#"):
		return fmt.Errorf("invalid image %q", s.Template.Image)
	case strings.TrimSpace(s.Template.Start) == "":
		return errors.New("template has no start command")
	case s.Limits.MemoryMB < minMemoryMB:
		return fmt.Errorf("memory limit %d MB is below the %d MB minimum", s.Limits.MemoryMB, minMemoryMB)
	case s.Limits.CPUMillicores < 0 || s.Limits.Pids < 0:
		return errors.New("negative limit")
	case s.Limits.DiskMB < minDiskMB:
		return fmt.Errorf("disk limit %d MB is below the %d MB minimum", s.Limits.DiskMB, minDiskMB)
	case s.Desired != proto.DesiredRunning && s.Desired != proto.DesiredStopped:
		return fmt.Errorf("invalid desired state %q", s.Desired)
	case s.DeployID != "" && (!validBotID(s.DeployID) || !strings.HasPrefix(s.ArtifactPath, "/")):
		return errors.New("deploy needs a valid deploy id and an absolute artifact path")
	case s.DeployID != "" && len(s.ArtifactSHA256) != 64:
		return errors.New("deploy needs the artifact's sha256")
	}
	for k, v := range s.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return fmt.Errorf("invalid env var %q", k)
		}
	}
	return nil
}

var hostnameRe = regexp.MustCompile(`[^a-z0-9-]+`)

func hostname(name string) string {
	h := strings.Trim(hostnameRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(h) > 63 {
		h = strings.Trim(h[:63], "-")
	}
	if h == "" {
		return "bot"
	}
	return h
}

// HostSampler computes node-wide stats; CPU percent is relative to the previous Sample call.
type HostSampler struct {
	DataDir  string
	prevBusy uint64
	prevAll  uint64
}

// HostInfo is what the node reports in hello.
type HostInfo struct {
	Hostname      string
	Kernel        string
	DockerVersion string
	CgroupV2      bool
	Runsc         bool
	CPUs          int
	MemoryBytes   int64
	DiskBytes     int64
}

const mib = 1 << 20

// LogEntry is one line of output with Docker's full-precision timestamp, which Logs(since)
// needs to resume a stream without repeating lines.
type LogEntry struct {
	Time   time.Time
	Stream string // stdout | stderr
	Text   string
}

func (l LogEntry) Line() proto.LogLine {
	return proto.LogLine{T: l.Time.UnixMilli(), Stream: l.Stream, Text: l.Text}
}
