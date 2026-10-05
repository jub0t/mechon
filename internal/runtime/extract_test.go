package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	typ      byte
	body     string
	linkname string
	mode     int64
}

func makeTarGz(t testing.TB, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
			if typ == tar.TypeDir {
				mode = 0o755
			}
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.linkname}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractGood(t *testing.T) {
	dir := t.TempDir()
	uid := testUID()
	data := makeTarGz(t, []tarEntry{
		{name: "./", typ: tar.TypeDir},
		{name: "index.js", body: "console.log(1)"},
		{name: "src/lib/a.js", body: "a"},
		{name: "bin/", typ: tar.TypeDir},
		{name: "bin/run", body: "#!/bin/sh", mode: 0o4755}, // setuid must be dropped
		{name: "link", typ: tar.TypeSymlink, linkname: "src/lib/a.js"},
		{name: "src/up", typ: tar.TypeSymlink, linkname: "../index.js"},
		{name: "hard", typ: tar.TypeLink, linkname: "index.js"},
	})
	if err := extractTarGz(bytes.NewReader(data), dir, 1<<20, uid); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "link"))
	if err != nil || string(b) != "a" {
		t.Fatalf("symlink: %q %v", b, err)
	}
	st, err := os.Stat(filepath.Join(dir, "bin/run"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		t.Fatalf("setuid bit kept: %v", st.Mode())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hard")); string(b) != "console.log(1)" {
		t.Fatalf("hardlink: %q", b)
	}
}

func TestExtractRejectsMalicious(t *testing.T) {
	cases := map[string][]tarEntry{
		"absolute path":       {{name: "/etc/passwd", body: "x"}},
		"dotdot":              {{name: "../escape", body: "x"}},
		"nested dotdot":       {{name: "a/../../escape", body: "x"}},
		"absolute symlink":    {{name: "l", typ: tar.TypeSymlink, linkname: "/etc"}},
		"escaping symlink":    {{name: "a/l", typ: tar.TypeSymlink, linkname: "../../etc"}},
		"write through link":  {{name: "l", typ: tar.TypeSymlink, linkname: "."}, {name: "l/l2", typ: tar.TypeSymlink, linkname: ".."}, {name: "l/l2/escape", body: "x"}},
		"hardlink outside":    {{name: "h", typ: tar.TypeLink, linkname: "../../etc/passwd"}},
		"hardlink absolute":   {{name: "h", typ: tar.TypeLink, linkname: "/etc/passwd"}},
		"char device":         {{name: "dev", typ: tar.TypeChar}},
		"block device":        {{name: "dev", typ: tar.TypeBlock}},
		"fifo":                {{name: "fifo", typ: tar.TypeFifo}},
		"over quota":          {{name: "big", body: strings.Repeat("x", 2048)}},
		"overwrite via link":  {{name: "x", typ: tar.TypeSymlink, linkname: "y"}, {name: "x", body: "z"}},
		"dir symlink then up": {{name: "d", typ: tar.TypeSymlink, linkname: "."}, {name: "d/../escape", body: "x"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "root")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			err := extractTarGz(bytes.NewReader(makeTarGz(t, entries)), dir, 1024, testUID())
			if err == nil {
				t.Fatal("extracted a malicious archive")
			}
			// Nothing may have appeared outside the root.
			ents, _ := os.ReadDir(parent)
			if len(ents) != 1 {
				t.Fatalf("files escaped the root: %v", ents)
			}
			if name != "over quota" && name != "overwrite via link" && !errors.Is(err, errUnsafeArchive) {
				t.Logf("error (not errUnsafeArchive): %v", err)
			}
		})
	}
}

func TestExtractNotGzip(t *testing.T) {
	if err := extractTarGz(strings.NewReader("PK\x03\x04 zip"), t.TempDir(), 1<<20, testUID()); err == nil {
		t.Fatal("accepted a non-gzip artifact")
	}
}

func TestDemuxLines(t *testing.T) {
	var buf bytes.Buffer
	frame := func(stream byte, s string) {
		var h [8]byte
		h[0] = stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
		buf.Write(h[:])
		buf.WriteString(s)
	}
	frame(1, "hello\nwor")
	frame(2, "err line\n")
	frame(1, "ld\n")
	frame(1, "no newline at end")
	var got []string
	if err := demuxLines(&buf, func(stream string, line []byte) { got = append(got, stream+":"+string(line)) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"stdout:hello", "stderr:err line", "stdout:world", "stdout:no newline at end"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateSpec(t *testing.T) {
	good := testSpecBase("bot-1", 100001)
	if err := ValidateSpec(good); err != nil {
		t.Fatal(err)
	}
	bad := []func(*protoBotSpec){
		func(s *protoBotSpec) { s.BotID = "../etc" },
		func(s *protoBotSpec) { s.BotID = "a/b" },
		func(s *protoBotSpec) { s.UID = 0 },
		func(s *protoBotSpec) { s.UID = 999 },
		func(s *protoBotSpec) { s.Limits.MemoryMB = 0 },
		func(s *protoBotSpec) { s.Limits.DiskMB = 1 },
		func(s *protoBotSpec) { s.Desired = "maybe" },
		func(s *protoBotSpec) { s.Env = map[string]string{"A=B": "x"} },
		func(s *protoBotSpec) {
			s.DeployID = "d1"
			s.ArtifactPath = "http://evil/x"
			s.ArtifactSHA256 = strings.Repeat("a", 64)
		},
		func(s *protoBotSpec) { s.DeployID = "d1"; s.ArtifactPath = "/a"; s.ArtifactSHA256 = "" },
	}
	for i, f := range bad {
		s := good
		f(&s)
		if ValidateSpec(s) == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if h := hostname("My Cool_Bot!!"); h != "my-cool-bot" {
		t.Errorf("hostname %q", h)
	}
}

// testUID is the owner extraction should chown to: -1 (leave as is) unless we are root.
func testUID() int {
	if os.Geteuid() == 0 {
		return 100999
	}
	return -1
}
