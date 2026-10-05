//go:build linux

package runtime

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jub0t/mechon/internal/proto"
)

func fileHash(t *testing.T, p string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func ownedBy(t *testing.T, p string, uid int, mode os.FileMode) {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		t.Fatal(err)
	}
	if int(st.Uid) != uid || int(st.Gid) != uid || os.FileMode(st.Mode&0o7777) != mode {
		t.Fatalf("%s: %d:%d %o, want %d:%d %o", p, st.Uid, st.Gid, st.Mode&0o7777, uid, uid, mode)
	}
}

// TestFilesVolume runs the file manager against a real loop-mounted bot volume, as root.
func TestFilesVolume(t *testing.T) {
	e := newTestEnv(t)
	d, ctx := e.d, e.ctx
	const uid = 100201
	spec := testSpecBase("files-1", uid)

	if _, err := d.ListFiles(ctx, spec.BotID, ""); !errors.Is(err, ErrNoFiles) || err.Error() != "this bot has no files yet" {
		t.Fatalf("no volume: %v", err)
	}
	if _, err := d.ReadFile(ctx, spec.BotID, "app/x"); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("no volume read: %v", err)
	}
	e.apply(spec) // no deploy: volume only
	mnt := d.mountPath(spec.BotID)
	app := filepath.Join(mnt, "app")

	l, err := d.ListFiles(ctx, spec.BotID, "")
	if err != nil || len(l.Entries) != 2 || l.Entries[0].Name != "app" || l.Entries[1].Name != "data" {
		t.Fatalf("roots: %+v %v", l, err)
	}

	// Happy path in both roots.
	if err := d.MakeDir(ctx, spec.BotID, "app/src", uid); err != nil {
		t.Fatal(err)
	}
	ownedBy(t, filepath.Join(app, "src"), uid, 0o755)
	if err := d.WriteFile(ctx, spec.BotID, "app/src/index.js", []byte("console.log(1)\n"), uid); err != nil {
		t.Fatal(err)
	}
	ownedBy(t, filepath.Join(app, "src", "index.js"), uid, 0o644)
	if err := d.WriteFile(ctx, spec.BotID, "app/run.sh", []byte("echo hi\n"), uid); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(app, "run.sh"), 0o755)
	if err := d.WriteFile(ctx, spec.BotID, "app/run.sh", []byte("echo v2\n"), uid); err != nil {
		t.Fatal(err)
	}
	ownedBy(t, filepath.Join(app, "run.sh"), uid, 0o755)
	if err := d.WriteFile(ctx, spec.BotID, "data/state.json", []byte(`{"n":1}`), uid); err != nil {
		t.Fatal(err)
	}
	ownedBy(t, filepath.Join(mnt, "data", "state.json"), uid, 0o644)

	l, err = d.ListFiles(ctx, spec.BotID, "app")
	if err != nil || l.Path != "app" || len(l.Entries) != 2 || l.Entries[0].Name != "src" || !l.Entries[0].Dir || l.Entries[1].Name != "run.sh" || l.Entries[1].Mode != 0o755 {
		t.Fatalf("list app: %+v %v", l, err)
	}
	c, err := d.ReadFile(ctx, spec.BotID, "app/run.sh")
	if err != nil || string(c.Content) != "echo v2\n" || c.Path != "app/run.sh" {
		t.Fatalf("read: %+v %v", c, err)
	}
	if err := d.DeleteFile(ctx, spec.BotID, "app/src"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(app, "src")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("app/src not deleted")
	}
	for _, p := range []string{"app", "data"} {
		if err := d.DeleteFile(ctx, spec.BotID, p); err == nil {
			t.Fatalf("deleted root %s", p)
		}
	}

	// Escapes by path: refused before anything is touched.
	metaBefore := fileHash(t, "/etc/passwd")
	os.WriteFile(filepath.Join(mnt, ".mechon", "deploy"), []byte("dep-x\n"), 0o600)
	for _, p := range []string{"../../etc/passwd", "/etc/passwd", ".mechon/deploy", ".mechon", "app/../../x", "app/../.mechon/deploy", "data/../../../etc/passwd", "app\\..\\x", "app/x\x00"} {
		if _, err := d.ListFiles(ctx, spec.BotID, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("list %q: %v", p, err)
		}
		if _, err := d.ReadFile(ctx, spec.BotID, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("read %q: %v", p, err)
		}
		if err := d.WriteFile(ctx, spec.BotID, p, []byte("pwned"), uid); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("write %q: %v", p, err)
		}
		if err := d.DeleteFile(ctx, spec.BotID, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("delete %q: %v", p, err)
		}
		if err := d.MakeDir(ctx, spec.BotID, p, uid); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("mkdir %q: %v", p, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(mnt, ".mechon", "deploy")); string(b) != "dep-x\n" {
		t.Fatalf(".mechon/deploy changed: %q", b)
	}
	if fileHash(t, "/etc/passwd") != metaBefore {
		t.Fatal("/etc/passwd changed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(d.dataDir), "x")); err == nil {
		t.Fatal("a file appeared next to the data dir")
	}

	// Escapes by symlinks the bot planted (created as the bot would: owned by its UID).
	shadow := fileHash(t, "/etc/shadow")
	var shadowSt syscall.Stat_t
	syscall.Stat("/etc/shadow", &shadowSt)
	plant := func(target, name string) {
		p := filepath.Join(app, name)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		os.Lchown(p, uid, uid)
	}
	plant("/etc/shadow", "shadow")
	plant("../../../../../../etc/shadow", "shadow-rel")
	plant("/", "rootfs")
	plant("../.mechon", "meta")
	for _, name := range []string{"shadow", "shadow-rel"} {
		if _, err := d.ReadFile(ctx, spec.BotID, "app/"+name); err == nil {
			t.Errorf("read through %s succeeded", name)
		}
		if err := d.WriteFile(ctx, spec.BotID, "app/"+name, []byte("root::0:0:::::\n"), uid); err == nil {
			t.Errorf("write through %s succeeded", name)
		}
	}
	for _, p := range []string{"app/rootfs", "app/rootfs/etc", "app/meta"} {
		if _, err := d.ListFiles(ctx, spec.BotID, p); err == nil {
			t.Errorf("list %s succeeded", p)
		}
	}
	for _, p := range []string{"app/rootfs/etc/shadow", "app/rootfs/etc/passwd", "app/meta/deploy"} {
		if _, err := d.ReadFile(ctx, spec.BotID, p); err == nil {
			t.Errorf("read %s succeeded", p)
		}
		if err := d.WriteFile(ctx, spec.BotID, p, []byte("pwned"), uid); err == nil {
			t.Errorf("write %s succeeded", p)
		}
		if err := d.DeleteFile(ctx, spec.BotID, p); err == nil {
			t.Errorf("delete %s succeeded", p)
		}
	}
	if err := d.MakeDir(ctx, spec.BotID, "app/rootfs/tmp/mechon-pwned", uid); err == nil {
		t.Error("mkdir through rootfs succeeded")
	}
	if _, err := os.Lstat("/tmp/mechon-pwned"); err == nil {
		t.Fatal("/tmp/mechon-pwned created")
	}
	// Listed, flagged as links.
	l, _ = d.ListFiles(ctx, spec.BotID, "app")
	links := 0
	for _, en := range l.Entries {
		if en.Link {
			links++
		}
	}
	if links != 4 {
		t.Fatalf("links listed: %+v", l.Entries)
	}
	// Deleting the links removes only the links.
	for _, name := range []string{"shadow", "shadow-rel", "rootfs", "meta"} {
		if err := d.DeleteFile(ctx, spec.BotID, "app/"+name); err != nil {
			t.Fatalf("delete link %s: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(app, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("link %s still there", name)
		}
	}
	var after syscall.Stat_t
	if err := syscall.Stat("/etc/shadow", &after); err != nil {
		t.Fatal(err)
	}
	if fileHash(t, "/etc/shadow") != shadow || after.Mode != shadowSt.Mode || after.Uid != shadowSt.Uid || after.Mtim != shadowSt.Mtim {
		t.Fatal("/etc/shadow changed")
	}
	if b, _ := os.ReadFile(filepath.Join(mnt, ".mechon", "deploy")); string(b) != "dep-x\n" {
		t.Fatalf(".mechon/deploy changed: %q", b)
	}
	for _, p := range []string{"/etc", "/etc/passwd", "/tmp"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s gone: %v", p, err)
		}
	}

	// Size limits and binary detection.
	big := bytes.Repeat([]byte("a"), proto.MaxFileWrite+1)
	if err := d.WriteFile(ctx, spec.BotID, "data/big", big, uid); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize write: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(mnt, "data", "big")); err == nil {
		t.Fatal("oversize write created a file")
	}
	os.WriteFile(filepath.Join(mnt, "data", "big"), big, 0o644)
	if c, err := d.ReadFile(ctx, spec.BotID, "data/big"); err != nil || !c.Truncated || len(c.Content) != 0 {
		t.Fatalf("oversize read: truncated=%v len=%d %v", c.Truncated, len(c.Content), err)
	}
	os.WriteFile(filepath.Join(mnt, "data", "blob"), []byte("\x7fELF\x02\x01\x01\x00\x00"), 0o644)
	if c, err := d.ReadFile(ctx, spec.BotID, "data/blob"); err != nil || !c.Binary || len(c.Content) != 0 {
		t.Fatalf("binary read: %+v %v", c, err)
	}
	if err := d.WriteFile(ctx, spec.BotID, "app/x", []byte("x"), 0); err == nil {
		t.Fatal("write with uid 0 accepted")
	}
}

// TestFilesVolumeFull fills a small volume through WriteFile until the disk is full: the
// error is ENOSPC, no temp files are left, and freeing space makes writes work again.
func TestFilesVolumeFull(t *testing.T) {
	e := newTestEnv(t)
	d, ctx := e.d, e.ctx
	const uid = 100202
	spec := testSpecBase("files-full", uid)
	spec.Limits.DiskMB = minDiskMB
	e.apply(spec)

	chunk := bytes.Repeat([]byte("0123456789abcdef"), proto.MaxFileWrite/16)
	var full error
	n := 0
	for ; n < 4*minDiskMB; n++ {
		if err := d.WriteFile(ctx, spec.BotID, fmt.Sprintf("data/f%03d", n), chunk, uid); err != nil {
			full = err
			break
		}
	}
	if !errors.Is(full, syscall.ENOSPC) {
		t.Fatalf("after %d files: want ENOSPC, got %v", n, full)
	}
	t.Logf("disk full after %d files: %v", n, full)
	if strings.Contains(full.Error(), d.dataDir) {
		t.Fatalf("error leaks host path: %v", full)
	}
	ents, _ := os.ReadDir(filepath.Join(d.mountPath(spec.BotID), "data"))
	for _, en := range ents {
		if strings.Contains(en.Name(), ".mechon-") {
			t.Fatalf("temp file left: %s", en.Name())
		}
	}
	if _, err := os.Lstat(filepath.Join(d.mountPath(spec.BotID), "data", fmt.Sprintf("f%03d", n))); err == nil {
		t.Fatal("failed write left the target file")
	}
	if err := d.DeleteFile(ctx, spec.BotID, "data/f000"); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFile(ctx, spec.BotID, "data/after", []byte("ok"), uid); err != nil {
		t.Fatalf("write after freeing space: %v", err)
	}
}
