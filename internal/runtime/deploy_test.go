//go:build linux

package runtime

import (
	"archive/tar"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

func TestDeploy(t *testing.T) {
	e := newTestEnv(t)
	d := e.d

	spec := testSpecBase("dep-1", 100201)
	spec.Template = proto.Template{
		ID:      "discord-js",
		Image:   "node:22-bookworm-slim",
		Install: `echo "installing as $(id -u):$(id -g) in $PWD home=$HOME"; mkdir -p node_modules/x && echo v1-installed > node_modules/x/marker; node --version`,
		Start:   `node ${MAIN:-index.js}`,
	}
	spec.Limits.MemoryMB = 256
	files := func(version string) []tarEntry {
		return []tarEntry{
			{name: "index.js", body: `const fs = require('fs');
console.log('running ` + version + ` marker=' + fs.readFileSync('node_modules/x/marker', 'utf8').trim() + ' uid=' + process.getuid());
setInterval(() => {}, 1e6);`},
			{name: "lib/", typ: tar.TypeDir},
			{name: "lib/util.js", body: "module.exports = 1"},
		}
	}

	// 1. A good deploy: fetch, install (in a throwaway container), swap, start.
	e.panel.publish(&spec, "d1", makeTarGz(t, files("v1")))
	inst := e.apply(spec)
	if !inst.Running || inst.DeployID != "d1" {
		t.Fatalf("after d1: %+v", inst)
	}
	if got := e.rec.phases("d1"); !slices.Equal(got, []string{proto.PhaseFetching, proto.PhaseInstalling, proto.PhaseLive}) {
		t.Fatalf("d1 phases %v", got)
	}
	out := e.rec.deployText("d1")
	if !strings.Contains(out, "installing as 100201:100201 in /home/container home=/home/container") {
		t.Fatalf("install output:\n%s", out)
	}
	waitFor(t, "v1 running", 20*time.Second, func() bool {
		return strings.Contains(logText(t, d, spec.BotID), "running v1 marker=v1-installed uid=100201")
	})
	if cs, _ := d.api.listContainers(e.ctx, labelInstall); len(cs) != 0 {
		t.Fatalf("install container left behind: %+v", cs)
	}
	goodID := inst.ContainerID

	// The running version must survive every failed deploy below.
	stillV1 := func(t *testing.T, deployID string) {
		t.Helper()
		if got := e.rec.phases(deployID); len(got) == 0 || got[len(got)-1] != proto.PhaseFailed {
			t.Errorf("%s: phases %v, want ending in failed", deployID, got)
		}
		c, err := d.api.inspectContainer(e.ctx, containerName(spec.BotID))
		if err != nil || c.ID != goodID || !c.State.Running {
			t.Errorf("%s: running container changed: %v", deployID, err)
		}
		if got := d.readDeployID(spec.BotID); got != "d1" {
			t.Errorf("%s: recorded deploy is %q", deployID, got)
		}
		if _, err := os.Stat(filepath.Join(d.metaDir(spec.BotID), "next")); !os.IsNotExist(err) {
			t.Errorf("%s: .mechon/next left behind", deployID)
		}
		if b, err := os.ReadFile(filepath.Join(d.appDir(spec.BotID), "node_modules/x/marker")); err != nil || strings.TrimSpace(string(b)) != "v1-installed" {
			t.Errorf("%s: app/ changed: %q %v", deployID, b, err)
		}
	}
	failDeploy := func(t *testing.T, s proto.BotSpec) string {
		t.Helper()
		_, err := d.Apply(e.ctx, s)
		if err == nil {
			t.Fatalf("%s: deploy succeeded, must fail", s.DeployID)
		}
		t.Logf("%s: %v", s.DeployID, err)
		return err.Error()
	}

	t.Run("sha256 mismatch", func(t *testing.T) {
		s := spec
		e.panel.publish(&s, "d-badsum", makeTarGz(t, files("v2")))
		s.ArtifactSHA256 = strings.Repeat("0", 64)
		if msg := failDeploy(t, s); !strings.Contains(msg, "sha256 mismatch") {
			t.Errorf("error: %s", msg)
		}
		stillV1(t, "d-badsum")
	})

	t.Run("install fails", func(t *testing.T) {
		s := spec
		s.Template.Install = "echo about to fail; exit 3"
		e.panel.publish(&s, "d-instfail", makeTarGz(t, files("v2")))
		if msg := failDeploy(t, s); !strings.Contains(msg, "exited with code 3") {
			t.Errorf("error: %s", msg)
		}
		if !strings.Contains(e.rec.deployText("d-instfail"), "about to fail") {
			t.Errorf("install output not streamed")
		}
		stillV1(t, "d-instfail")
	})

	t.Run("not a tar.gz", func(t *testing.T) {
		s := spec
		e.panel.publish(&s, "d-zip", []byte("PK\x03\x04 this is a zip"))
		failDeploy(t, s)
		stillV1(t, "d-zip")
	})

	t.Run("artifact missing on panel", func(t *testing.T) {
		s := spec
		e.panel.publish(&s, "d-404", makeTarGz(t, files("v2")))
		s.ArtifactPath = "/agent/v1/artifacts/nope"
		if msg := failDeploy(t, s); !strings.Contains(msg, "HTTP 404") {
			t.Errorf("error: %s", msg)
		}
		stillV1(t, "d-404")
	})

	malicious := map[string][]tarEntry{
		"d-traversal":    {{name: "index.js", body: "x"}, {name: "../../../../escape-traversal", body: "pwned"}},
		"d-absolute":     {{name: "/escape-absolute", body: "pwned"}},
		"d-symlink":      {{name: "index.js", body: "x"}, {name: "evil", typ: tar.TypeSymlink, linkname: "../../../../../../etc"}, {name: "evil/escape-symlink", body: "pwned"}},
		"d-symlink-abs":  {{name: "etc", typ: tar.TypeSymlink, linkname: "/etc"}, {name: "etc/escape-symlink-abs", body: "pwned"}},
		"d-symlink-dir":  {{name: "up", typ: tar.TypeSymlink, linkname: "."}, {name: "up/up2", typ: tar.TypeSymlink, linkname: ".."}, {name: "up/up2/escape-chain", body: "pwned"}},
		"d-hardlink":     {{name: "shadow", typ: tar.TypeLink, linkname: "../../../../../../etc/shadow"}},
		"d-device":       {{name: "mem", typ: tar.TypeChar}},
		"d-quota-bomb":   {{name: "big", body: strings.Repeat("A", 65*mib)}}, // DiskMB is 64
		"d-symlink-host": {{name: "data", typ: tar.TypeSymlink, linkname: "../data/../../../../"}},
	}
	for id, entries := range malicious {
		t.Run("malicious "+id, func(t *testing.T) {
			s := spec
			e.panel.publish(&s, id, makeTarGz(t, entries))
			failDeploy(t, s)
			stillV1(t, id)
		})
	}
	// Nothing escaped anywhere we can think of.
	for _, p := range []string{"/escape-traversal", "/escape-absolute", "/etc/escape-symlink", "/etc/escape-symlink-abs",
		filepath.Join(d.dataDir, "escape-traversal"), filepath.Join(d.dataDir, "mounts", "escape-traversal"),
		filepath.Join(d.mountPath(spec.BotID), "escape-chain"), filepath.Join(d.metaDir(spec.BotID), "escape-chain")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("malicious archive wrote %s", p)
			os.Remove(p)
		}
	}

	// 2. A newer good deploy replaces v1; v1 is kept as prev for one generation.
	spec.Template.Install = `mkdir -p node_modules/x && echo v2-installed > node_modules/x/marker`
	e.panel.publish(&spec, "d2", makeTarGz(t, files("v2")))
	inst = e.apply(spec)
	if inst.DeployID != "d2" || inst.ContainerID == goodID || !inst.Running {
		t.Fatalf("after d2: %+v", inst)
	}
	waitFor(t, "v2 running", 20*time.Second, func() bool {
		return strings.Contains(logText(t, d, spec.BotID), "running v2 marker=v2-installed")
	})
	if b, err := os.ReadFile(filepath.Join(d.metaDir(spec.BotID), "prev", "node_modules/x/marker")); err != nil || !strings.Contains(string(b), "v1") {
		t.Fatalf("prev generation: %q %v", b, err)
	}
	// Same spec again: no redeploy.
	n := len(e.rec.phases("d2"))
	e.apply(spec)
	if len(e.rec.phases("d2")) != n {
		t.Fatalf("re-apply redeployed")
	}
}
