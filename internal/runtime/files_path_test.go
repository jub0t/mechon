package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/jub0t/mechon/internal/proto"
)

func TestSplitFilesPath(t *testing.T) {
	ok := map[string][2]string{
		"app":              {"app", "."},
		"data":             {"data", "."},
		"app/index.js":     {"app", "index.js"},
		"data/a/b/c.txt":   {"data", "a/b/c.txt"},
		"app/.env":         {"app", ".env"},
		"app/..hidden":     {"app", "..hidden"},
		"app/with space":   {"app", "with space"},
		"data/ünïcødé.txt": {"data", "ünïcødé.txt"},
	}
	for in, want := range ok {
		root, rel, err := splitFilesPath(in)
		if err != nil || root != want[0] || rel != want[1] {
			t.Errorf("%q: got %q %q %v, want %v", in, root, rel, err, want)
		}
	}
	bad := []string{
		"", "/", "/etc/passwd", "../../etc/passwd", "app/../../x", "app/..", "app/../data",
		".mechon", ".mechon/deploy", "./app", "app/.", "app/./x", "app/", "app//x", "/app/x",
		"app\\x", "app/x\x00y", "App", "apps/x", "dataa", "..", ".", "data/../.mechon/deploy",
		"app/\xff", strings.Repeat("a/", 3000) + "x",
	}
	for _, in := range bad {
		if _, _, err := splitFilesPath(in); !errors.Is(err, ErrInvalidPath) || err.Error() != "invalid path" {
			t.Errorf("%q accepted (err %v)", in, err)
		}
	}
}

func TestSortEntriesAndRoots(t *testing.T) {
	es := []proto.FileEntry{{Name: "b.txt"}, {Name: "z", Dir: true}, {Name: "a.txt"}, {Name: "a", Dir: true}, {Name: "link", Link: true}}
	sortEntries(es)
	var got []string
	for _, e := range es {
		got = append(got, e.Name)
	}
	if strings.Join(got, ",") != "a,z,a.txt,b.txt,link" {
		t.Fatalf("order %v", got)
	}
	r := rootListing()
	if len(r.Entries) != 2 || r.Entries[0].Name != "app" || r.Entries[1].Name != "data" || !r.Entries[0].Dir || !r.Entries[1].Dir {
		t.Fatalf("roots %+v", r)
	}
}

func TestIsBinary(t *testing.T) {
	cases := map[string]bool{
		"":                                   false,
		"hello\nworld":                       false,
		"héllo 🌍":                            false,
		"a\x00b":                             true,
		"\xff\xfe":                           true,
		strings.Repeat("x", 9000) + "\x00":   false, // NUL past the sniff window, still valid UTF-8
		strings.Repeat("x", 9000) + "\xc3(":  true,  // invalid UTF-8 anywhere
		"\x1b[31mcolored\x1b[0m":             false,
		string([]byte{0xe2, 0x82}) + "trail": true,
	}
	for in, want := range cases {
		if got := isBinary([]byte(in)); got != want {
			t.Errorf("isBinary(%q...) = %v", in[:min(len(in), 20)], got)
		}
	}
}
