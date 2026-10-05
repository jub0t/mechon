//go:build unix

package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jub0t/mechon/internal/proto"
)

// These run anywhere (no root, no volume): the file operations against an os.Root on a temp
// directory, with a sibling "outside" directory standing in for the host.

type rootFixture struct {
	t       *testing.T
	app     string // the root directory
	outside string
	secret  string // a file outside that must never change
	root    *os.Root
	uid     int // -1: chown is a no-op (ownership is checked by the Linux volume tests)
}

func newRootFixture(t *testing.T) *rootFixture {
	base := t.TempDir()
	f := &rootFixture{t: t, app: filepath.Join(base, "vol", "app"), outside: filepath.Join(base, "outside"), uid: -1}
	for _, d := range []string{f.app, f.outside, filepath.Join(f.outside, "dir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.secret = filepath.Join(f.outside, "secret")
	f.write(f.secret, "TOP SECRET")
	f.write(filepath.Join(f.outside, "dir", "keep"), "keep me")
	f.write(filepath.Join(f.app, "index.js"), "console.log('hi')\n")
	if err := os.Mkdir(filepath.Join(f.app, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.app, "src", "a.js"), "a")
	// What a bot can plant in its own files.
	f.link(f.secret, "abs-secret")
	f.link("../../outside/secret", "rel-secret")
	f.link("/", "rootdir")
	f.link(f.outside, "outdir")
	f.link("src", "inner") // stays inside, still never followed
	r, err := os.OpenRoot(f.app)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	f.root = r
	return f
}

func (f *rootFixture) write(p, s string) {
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *rootFixture) link(target, name string) {
	if err := os.Symlink(target, filepath.Join(f.app, name)); err != nil {
		f.t.Fatal(err)
	}
}

// untouched checks that nothing outside the root changed.
func (f *rootFixture) untouched() {
	f.t.Helper()
	if b, err := os.ReadFile(f.secret); err != nil || string(b) != "TOP SECRET" {
		f.t.Fatalf("secret changed: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(f.outside, "dir", "keep")); err != nil || string(b) != "keep me" {
		f.t.Fatalf("outside/dir/keep changed: %q %v", b, err)
	}
	ents, _ := os.ReadDir(f.outside)
	if len(ents) != 2 {
		f.t.Fatalf("outside dir has %d entries", len(ents))
	}
}

func TestFilesRootHappyPath(t *testing.T) {
	f := newRootFixture(t)

	l, err := listDir(f.root, ".", "app")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range l.Entries {
		names = append(names, fmt.Sprintf("%s:%v:%v", e.Name, e.Dir, e.Link))
	}
	want := "src:true:false,abs-secret:false:true,index.js:false:false,inner:false:true,outdir:false:true,rel-secret:false:true,rootdir:false:true"
	if strings.Join(names, ",") != want || l.Path != "app" {
		t.Fatalf("listing:\n got %s\nwant %s", strings.Join(names, ","), want)
	}

	c, err := readFile(f.root, "index.js", "app/index.js")
	if err != nil || string(c.Content) != "console.log('hi')\n" || c.Binary || c.Truncated || c.Size != 18 {
		t.Fatalf("read: %+v %v", c, err)
	}

	// New file: 0644. Overwrite keeps the mode.
	if err := writeFile(f.root, "src/new.txt", "app/src/new.txt", []byte("new"), f.uid); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(f.app, "src", "new.txt")); fi.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode %v", fi.Mode())
	}
	os.Chmod(filepath.Join(f.app, "index.js"), 0o600)
	if err := writeFile(f.root, "index.js", "app/index.js", []byte("v2"), f.uid); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(f.app, "index.js"))
	if b, _ := os.ReadFile(filepath.Join(f.app, "index.js")); string(b) != "v2" || fi.Mode().Perm() != 0o600 {
		t.Fatalf("overwrite: %q %v", b, fi.Mode())
	}
	// Empty content is a valid file.
	if err := writeFile(f.root, "empty", "app/empty", nil, f.uid); err != nil {
		t.Fatal(err)
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(f.app)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".mechon-") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}

	// Parent must exist; mkdir is separate.
	if err := writeFile(f.root, "nope/x", "app/nope/x", []byte("x"), f.uid); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("write without parent: %v", err)
	}
	if err := makeDir(f.root, "nope", "app/nope", f.uid); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(f.app, "nope")); !fi.IsDir() || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mkdir mode %v", fi.Mode())
	}
	if err := makeDir(f.root, "nope", "app/nope", f.uid); err == nil {
		t.Fatal("mkdir over existing dir succeeded")
	}
	if err := makeDir(f.root, "a/b", "app/a/b", f.uid); err == nil {
		t.Fatal("mkdir without parent succeeded")
	}
	if err := writeFile(f.root, "nope/x", "app/nope/x", []byte("x"), f.uid); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(f.root, "nope", "app/nope", []byte("x"), f.uid); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("write over a directory: %v", err)
	}

	// Delete a file, then a directory recursively.
	if err := deleteFile(f.root, "src/new.txt", "app/src/new.txt"); err != nil {
		t.Fatal(err)
	}
	if err := deleteFile(f.root, "nope", "app/nope"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.app, "nope")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dir still there: %v", err)
	}
	if err := deleteFile(f.root, "nope", "app/nope"); err == nil || !strings.Contains(err.Error(), "app/nope: no such file") {
		t.Fatalf("delete missing: %v", err)
	}
	if err := deleteFile(f.root, ".", "app"); err == nil {
		t.Fatal("deleted the root")
	}
	if _, err := os.Stat(f.app); err != nil {
		t.Fatal(err)
	}
	f.untouched()
}

func TestFilesRootSymlinksNeverFollowed(t *testing.T) {
	f := newRootFixture(t)
	for _, name := range []string{"abs-secret", "rel-secret"} {
		disp := "app/" + name
		if _, err := readFile(f.root, name, disp); !errors.Is(err, errSymlink) {
			t.Errorf("read %s: %v", name, err)
		}
		if err := writeFile(f.root, name, disp, []byte("pwned"), f.uid); !errors.Is(err, errSymlink) {
			t.Errorf("write %s: %v", name, err)
		}
		if err := makeDir(f.root, name+"/x", disp+"/x", f.uid); err == nil {
			t.Errorf("mkdir under %s succeeded", name)
		}
	}
	// Through a directory link, to / or outside or even inside the root: all refused.
	for _, p := range []string{"rootdir/etc/passwd", "outdir/secret", "outdir/dir/keep", "inner/a.js"} {
		disp := "app/" + p
		if _, err := readFile(f.root, p, disp); err == nil {
			t.Errorf("read %s succeeded", p)
		}
		if err := writeFile(f.root, p, disp, []byte("pwned"), f.uid); err == nil {
			t.Errorf("write %s succeeded", p)
		}
		if err := deleteFile(f.root, p, disp); err == nil {
			t.Errorf("delete %s succeeded", p)
		}
		if err := makeDir(f.root, p+"x", disp+"x", f.uid); err == nil {
			t.Errorf("mkdir %s succeeded", p)
		}
	}
	for _, p := range []string{"rootdir", "outdir", "inner", "rootdir/etc", "outdir/dir"} {
		if _, err := listDir(f.root, p, "app/"+p); err == nil {
			t.Errorf("list %s succeeded", p)
		}
	}
	// Raw os.Root lookups that would follow: still confined (defence in depth).
	if _, err := f.root.ReadFile("abs-secret"); err == nil {
		t.Error("os.Root followed an absolute link")
	}
	if _, err := f.root.ReadFile("rel-secret"); err == nil {
		t.Error("os.Root followed an escaping link")
	}
	f.untouched()

	// Deleting a link removes only the link.
	for _, name := range []string{"abs-secret", "rel-secret", "rootdir", "outdir", "inner"} {
		if err := deleteFile(f.root, name, "app/"+name); err != nil {
			t.Fatalf("delete link %s: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(f.app, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("link %s still there", name)
		}
	}
	if _, err := os.Stat(filepath.Join(f.app, "src", "a.js")); err != nil {
		t.Fatal("inner link target removed")
	}
	f.untouched()
}

func TestFilesRootRecursiveDeleteKeepsLinkTargets(t *testing.T) {
	f := newRootFixture(t)
	if err := os.MkdirAll(filepath.Join(f.app, "d", "e"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Symlink(f.outside, filepath.Join(f.app, "d", "e", "out"))
	os.Symlink(f.secret, filepath.Join(f.app, "d", "sec"))
	if err := deleteFile(f.root, "d", "app/d"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.app, "d")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("d still there")
	}
	f.untouched()
}

func TestFilesRootLimits(t *testing.T) {
	f := newRootFixture(t)
	// Write over the limit refused, nothing created.
	big := bytes.Repeat([]byte("a"), proto.MaxFileWrite+1)
	if err := writeFile(f.root, "big", "app/big", big, f.uid); !errors.Is(err, errTooLarge) {
		t.Fatalf("oversize write: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.app, "big")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("oversize write left a file")
	}
	if err := writeFile(f.root, "max", "app/max", big[:proto.MaxFileWrite], f.uid); err != nil {
		t.Fatalf("write at the limit: %v", err)
	}
	// Oversize read: truncated, no content.
	os.WriteFile(filepath.Join(f.app, "big"), big, 0o644)
	c, err := readFile(f.root, "big", "app/big")
	if err != nil || !c.Truncated || c.Content != nil || c.Size != int64(len(big)) {
		t.Fatalf("oversize read: %+v %v", c.Truncated, err)
	}
	// Binary: no content.
	os.WriteFile(filepath.Join(f.app, "bin"), []byte("ELF\x00\x01\x02"), 0o644)
	c, err = readFile(f.root, "bin", "app/bin")
	if err != nil || !c.Binary || c.Content != nil {
		t.Fatalf("binary read: %+v %v", c, err)
	}
	os.WriteFile(filepath.Join(f.app, "latin1"), []byte("caf\xe9"), 0o644)
	if c, _ = readFile(f.root, "latin1", "app/latin1"); !c.Binary {
		t.Fatal("invalid UTF-8 not detected as binary")
	}
	// A FIFO is refused and does not hang.
	if err := syscall.Mkfifo(filepath.Join(f.app, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFile(f.root, "fifo", "app/fifo"); !errors.Is(err, errNotRegular) {
		t.Fatalf("fifo read: %v", err)
	}
	if err := writeFile(f.root, "fifo", "app/fifo", []byte("x"), f.uid); !errors.Is(err, errNotRegular) {
		t.Fatalf("fifo write: %v", err)
	}
	// Reading a directory.
	if _, err := readFile(f.root, "src", "app/src"); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("dir read: %v", err)
	}
	// Listing a file.
	if _, err := listDir(f.root, "index.js", "app/index.js"); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("list a file: %v", err)
	}
}

func TestFilesRootListCap(t *testing.T) {
	f := newRootFixture(t)
	dir := filepath.Join(f.app, "many")
	os.Mkdir(dir, 0o755)
	for i := range maxListEntries + 100 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(dir, "zdir"), 0o755)
	l, err := listDir(f.root, "many", "app/many")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != maxListEntries || l.Entries[0].Name != "zdir" || !l.Entries[0].Dir || l.Entries[1].Name != "f00000" {
		t.Fatalf("cap: %d entries, first %+v", len(l.Entries), l.Entries[0])
	}
}

func TestFilesErrNoHostPaths(t *testing.T) {
	f := newRootFixture(t)
	_, err := readFile(f.root, "missing", "app/missing")
	if err == nil || strings.Contains(err.Error(), f.app) || err.Error() != "app/missing: no such file or directory" {
		t.Fatalf("error leaks or is unclear: %v", err)
	}
}
