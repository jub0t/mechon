// Package proto is the wire protocol between the panel and the node agent (spec §4).
//
// The agent dials wss://<panel>/agent/v1/connect with "Authorization: Bearer <node token>" and
// both sides exchange JSON text frames shaped as Envelope. Requests carry an ID and are answered
// with TypeOK or TypeError echoing it; events carry no ID.
//
// The protocol is declarative: the panel sends the full desired spec of each bot and the agent
// converges on it. TypeSync after every connect carries the complete set for the node, so a
// dropped connection or an agent restart heals itself.
package proto

import (
	"encoding/json"
	"time"
)

// Version is bumped on incompatible changes. The panel refuses agents speaking another version.
const Version = 1

type Envelope struct {
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Agent → panel.
const (
	TypeHello          = "hello"           // Hello, first frame after connecting
	TypeBotState       = "bot.state"       // BotState, on every lifecycle change
	TypeStats          = "stats"           // Stats, every StatsInterval
	TypeLog            = "log"             // LogBatch, while a logs subscription is open
	TypeDeployProgress = "deploy.progress" // DeployProgress, during a deploy
	TypeOK             = "ok"              // reply to a request; Data is empty or request-specific
	TypeError          = "error"           // reply to a request; Data is ErrorReply
)

// Panel → agent. All are requests (carry an ID).
const (
	TypeSync            = "sync"             // Sync
	TypeBotApply        = "bot.apply"        // BotSpec
	TypeBotRemove       = "bot.remove"       // BotRef
	TypeBotRestart      = "bot.restart"      // BotRef
	TypeLogsSubscribe   = "logs.subscribe"   // LogsSubscribe; reply Data is LogBatch with the tail
	TypeLogsUnsubscribe = "logs.unsubscribe" // BotRef
)

const (
	StatsInterval = 5 * time.Second
	PingInterval  = 15 * time.Second
	DeadAfter     = 45 * time.Second
)

// ---------- Agent → panel ----------

type Hello struct {
	Protocol      int    `json:"protocol"`
	AgentVersion  string `json:"agentVersion"`
	Hostname      string `json:"hostname"`
	Kernel        string `json:"kernel"`
	DockerVersion string `json:"dockerVersion"`
	CgroupV2      bool   `json:"cgroupV2"`
	Runsc         bool   `json:"runsc"` // gVisor runtime registered with Docker
	CPUs          int    `json:"cpus"`
	MemoryBytes   int64  `json:"memoryBytes"`
	DiskBytes     int64  `json:"diskBytes"` // size of the filesystem holding the data dir
	// Bots the agent currently manages, so the panel can reconcile its view before Sync.
	Bots []BotState `json:"bots"`
}

// Observed bot states.
const (
	StatePending    = "pending"    // known, nothing running yet (e.g. no deploy)
	StateInstalling = "installing" // running the template's install step
	StateRunning    = "running"
	StateStopped    = "stopped" // stopped on purpose
	StateCrashed    = "crashed" // exited on its own; the agent will retry with backoff
)

type BotState struct {
	BotID     string    `json:"botId"`
	DeployID  string    `json:"deployId,omitempty"` // deploy currently live in the container
	State     string    `json:"state"`
	ExitCode  *int      `json:"exitCode,omitempty"`
	OOMKilled bool      `json:"oomKilled,omitempty"`
	Restarts  int       `json:"restarts"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

type Stats struct {
	At   time.Time  `json:"at"`
	Node NodeStats  `json:"node"`
	Bots []BotStats `json:"bots"`
}

type NodeStats struct {
	CPUPercent  float64 `json:"cpuPercent"` // of all cores, 0–100
	MemoryBytes int64   `json:"memoryBytes"`
	DiskBytes   int64   `json:"diskBytes"` // used on the data dir filesystem
}

type BotStats struct {
	BotID       string  `json:"botId"`
	CPUPercent  float64 `json:"cpuPercent"` // of the bot's own CPU limit, 0–100
	MemoryBytes int64   `json:"memoryBytes"`
	DiskBytes   int64   `json:"diskBytes"` // used inside the bot's volume
	NetRxBytes  int64   `json:"netRxBytes"`
	NetTxBytes  int64   `json:"netTxBytes"`
}

type LogLine struct {
	T      int64  `json:"t"`      // unix ms
	Stream string `json:"stream"` // stdout | stderr | system
	Text   string `json:"text"`
}

type LogBatch struct {
	BotID string    `json:"botId"`
	Lines []LogLine `json:"lines"`
}

// Deploy phases.
const (
	PhaseFetching   = "fetching"
	PhaseInstalling = "installing"
	PhaseLive       = "live"
	PhaseFailed     = "failed"
)

type DeployProgress struct {
	DeployID string    `json:"deployId"`
	BotID    string    `json:"botId"`
	Phase    string    `json:"phase"`
	Lines    []LogLine `json:"lines,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type ErrorReply struct {
	Message string `json:"message"`
}

// ---------- Panel → agent ----------

type Sync struct {
	Bots []BotSpec `json:"bots"`
}

type BotRef struct {
	BotID string `json:"botId"`
}

type LogsSubscribe struct {
	BotID string `json:"botId"`
	Tail  int    `json:"tail"`
}

// BotSpec is everything the agent needs to run one bot, and nothing else.
type BotSpec struct {
	BotID string `json:"botId"`
	Name  string `json:"name"` // display only (labels, hostname)
	// Linux UID and GID the bot's processes and files belong to. Unique per bot (100000 + seq).
	UID int `json:"uid"`

	// The deploy to run. Empty means no code has been deployed yet: the agent prepares the volume
	// and reports StatePending. A change of DeployID triggers fetch + install; any other change
	// only recreates the container.
	DeployID       string `json:"deployId,omitempty"`
	ArtifactPath   string `json:"artifactPath,omitempty"` // GET this on the panel URL with the node token
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`

	Template Template          `json:"template"`
	Limits   Limits            `json:"limits"`
	Hardened bool              `json:"hardened"` // run under gVisor (runsc)
	Env      map[string]string `json:"env"`
	Desired  string            `json:"desired"` // DesiredRunning | DesiredStopped
}

const (
	DesiredRunning = "running"
	DesiredStopped = "stopped"
)

type Template struct {
	ID      string `json:"id"`
	Image   string `json:"image"`
	Install string `json:"install"` // shell script run in /home/container with network on; may be empty
	Start   string `json:"start"`   // shell command that runs the bot
}

type Limits struct {
	MemoryMB      int `json:"memoryMb"`
	CPUMillicores int `json:"cpuMillicores"`
	DiskMB        int `json:"diskMb"`
	Pids          int `json:"pids"`
}

// ---------- Helpers ----------

func Encode(id, typ string, v any) ([]byte, error) {
	var data json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		data = b
	}
	return json.Marshal(Envelope{ID: id, Type: typ, Data: data})
}
