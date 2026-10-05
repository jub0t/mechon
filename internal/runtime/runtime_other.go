//go:build !linux

package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// The agent runs bots on Linux only (loop mounts, iptables, cgroup v2). These stubs keep the
// module building elsewhere, e.g. on a developer's Mac.

var errNotLinux = errors.New("mechon-agent runs on Linux only")

type Docker struct{}

func NewDocker(ctx context.Context, cfg Config) (*Docker, error) { return nil, errNotLinux }

func (*Docker) Apply(context.Context, proto.BotSpec) (Instance, error) {
	return Instance{}, errNotLinux
}
func (*Docker) Remove(context.Context, string) error              { return errNotLinux }
func (*Docker) Restart(context.Context, string) (Instance, error) { return Instance{}, errNotLinux }
func (*Docker) List(context.Context) ([]Instance, error)          { return nil, errNotLinux }
func (*Docker) Logs(context.Context, string, int, time.Time, bool) (<-chan LogEntry, error) {
	return nil, errNotLinux
}
func (*Docker) Stats(context.Context) ([]proto.BotStats, error) { return nil, errNotLinux }
func (*Docker) Events(context.Context) (<-chan Event, error)    { return nil, errNotLinux }
func (*Docker) ListFiles(context.Context, string, string) (proto.FilesListing, error) {
	return proto.FilesListing{}, errNotLinux
}
func (*Docker) ReadFile(context.Context, string, string) (proto.FileContent, error) {
	return proto.FileContent{}, errNotLinux
}
func (*Docker) WriteFile(context.Context, string, string, []byte, int) error { return errNotLinux }
func (*Docker) DeleteFile(context.Context, string, string) error             { return errNotLinux }
func (*Docker) MakeDir(context.Context, string, string, int) error           { return errNotLinux }
func (*Docker) HostInfo(context.Context) (HostInfo, error)                   { return HostInfo{}, errNotLinux }
func (*HostSampler) Sample() proto.NodeStats                                 { return proto.NodeStats{} }
