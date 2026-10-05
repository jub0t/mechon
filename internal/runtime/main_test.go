package runtime

import (
	"fmt"
	"os"
	"testing"

	"github.com/jub0t/mechon/internal/proto"
)

type protoBotSpec = proto.BotSpec

const testImage = "alpine:3.20"

func TestMain(m *testing.M) {
	// Helper mode for the setuid test: the test binary is copied into a bot and run there.
	if os.Getenv("MECHON_EUID_HELPER") == "1" {
		fmt.Printf("euid=%d\n", os.Geteuid())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testSpecBase(id string, uid int) proto.BotSpec {
	return proto.BotSpec{
		BotID:    id,
		Name:     "Test " + id,
		UID:      uid,
		Template: proto.Template{ID: "test", Image: testImage, Start: "echo started; exec sleep 3600"},
		Limits:   proto.Limits{MemoryMB: 128, CPUMillicores: 500, DiskMB: 64, Pids: 64},
		Env:      map[string]string{"BOT_ID": id},
		Desired:  proto.DesiredRunning,
	}
}
