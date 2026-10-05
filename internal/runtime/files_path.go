package runtime

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jub0t/mechon/internal/proto"
)

// File manager: path rules shared by every platform. A file-manager path is relative to the
// bot's volume and must start with one of fileRoots; "" stands for the list of roots.

var fileRoots = []string{"app", "data"}

const (
	maxListEntries = 2000 // a listing is cut after this many entries
	maxFilesPath   = 4096
	binarySniff    = 8 << 10
)

// Errors returned by the file operations. Their messages are shown to the panel's users, so
// they never contain host paths.
var (
	ErrInvalidPath = errors.New("invalid path")
	ErrNoFiles     = errors.New("this bot has no files yet")
	errSymlink     = errors.New("refusing to follow a symlink")
	errTooLarge    = errors.New("file is too large")
)

// splitFilesPath validates p and splits it into its root ("app" or "data") and the path below
// that root ("." for the root itself). It rejects absolute paths, ".", "..", empty segments
// (leading, trailing or doubled slashes), NUL bytes, backslashes and anything outside the
// two roots, e.g. ".mechon". The empty path is not valid here; callers handle it first.
func splitFilesPath(p string) (root, rel string, err error) {
	if p == "" || len(p) > maxFilesPath || !utf8.ValidString(p) ||
		strings.ContainsRune(p, 0) || strings.Contains(p, `\`) {
		return "", "", ErrInvalidPath
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", ErrInvalidPath
		}
	}
	if !slices.Contains(fileRoots, parts[0]) {
		return "", "", ErrInvalidPath
	}
	if len(parts) == 1 {
		return parts[0], ".", nil
	}
	return parts[0], strings.Join(parts[1:], "/"), nil
}

// rootListing is the listing of path "": the two roots, both directories.
func rootListing() proto.FilesListing {
	l := proto.FilesListing{Path: "", Entries: []proto.FileEntry{}}
	for _, r := range fileRoots {
		l.Entries = append(l.Entries, proto.FileEntry{Name: r, Dir: true, Mode: 0o755})
	}
	return l
}

// sortEntries orders directories first, then files, each by name.
func sortEntries(es []proto.FileEntry) {
	slices.SortFunc(es, func(a, b proto.FileEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// isBinary reports whether b should not be shown as text: a NUL byte in its first 8 KB, or
// invalid UTF-8 anywhere.
func isBinary(b []byte) bool {
	return bytes.IndexByte(b[:min(len(b), binarySniff)], 0) >= 0 || !utf8.Valid(b)
}
