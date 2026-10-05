// Package runtime runs bots. Runtime is the seam between the agent's reconciler and the
// container engine (spec §5); Docker is the only implementation in v0.
//
// The runtime is mechanics only: it converges one bot's volume, code and container onto a spec,
// and reports what the engine does. Decisions that need memory across events (restart backoff,
// restart counts) belong to the agent, which serialises all calls for a given bot.
package runtime

import (
	"context"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

type Runtime interface {
	// Apply converges one bot onto spec: volume (create, mount, grow), code (fetch + install +
	// swap when DeployID changed), container (recreate when its config changed) and run state
	// (start or stop per spec.Desired). Idempotent. A failed deploy leaves the previous version
	// in place; Apply still converges the container and then returns the deploy error.
	Apply(ctx context.Context, spec proto.BotSpec) (Instance, error)
	// Remove stops and deletes the bot's container and volume. Removing an unknown bot is a no-op.
	Remove(ctx context.Context, botID string) error
	// Restart stops (if running) and starts the bot's existing container.
	Restart(ctx context.Context, botID string) (Instance, error)
	// List returns every bot this node has a container or a volume for.
	List(ctx context.Context) ([]Instance, error)
	// Logs streams demultiplexed log lines of the bot's current container. The channel is
	// closed when the stream ends (container gone, follow=false done, or ctx cancelled). Lines at
	// or before since are skipped.
	Logs(ctx context.Context, botID string, tail int, since time.Time, follow bool) (<-chan LogEntry, error)
	// Stats samples every running bot. CPU percent is computed against the previous call.
	Stats(ctx context.Context) ([]proto.BotStats, error)
	// Events reports container exits. Exits caused by the runtime itself (stop, recreate,
	// remove) are not reported. Must be called once.
	Events(ctx context.Context) (<-chan Event, error)

	// File manager. Paths follow proto's rules: "app/..." or "data/..." inside the bot's
	// volume ("" lists the two roots for ListFiles). Every operation is confined to the volume
	// and never follows symlinks. A bot without a volume yields ErrNoFiles, a malformed path
	// ErrInvalidPath. uid is the bot's UID (and GID), the owner of what gets created.
	ListFiles(ctx context.Context, botID, path string) (proto.FilesListing, error)
	ReadFile(ctx context.Context, botID, path string) (proto.FileContent, error)
	// WriteFile atomically replaces or creates a regular file; its parent must exist.
	WriteFile(ctx context.Context, botID, path string, content []byte, uid int) error
	// DeleteFile removes a file, a symlink (not its target) or a directory recursively.
	DeleteFile(ctx context.Context, botID, path string) error
	// MakeDir creates one directory; its parent must exist.
	MakeDir(ctx context.Context, botID, path string, uid int) error
}

// Instance is what the runtime currently has for a bot.
type Instance struct {
	BotID       string
	ContainerID string // empty when there is no container (e.g. nothing deployed yet)
	DeployID    string // deploy whose code is in the volume
	Running     bool
	ExitCode    int
	OOMKilled   bool
	HasVolume   bool
}

// Event is an unexpected container exit.
type Event struct {
	BotID       string
	ContainerID string
	ExitCode    int
	OOMKilled   bool
	At          time.Time
}

// Hooks receive output the runtime produces while working. Calls must not block for long.
type Hooks struct {
	DeployProgress func(proto.DeployProgress)
	SystemLog      func(botID, text string)
}
