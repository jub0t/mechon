//go:build unix

package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/jub0t/mechon/internal/proto"
)

// File-manager operations on one of a bot's roots (app/ or data/), opened as an os.Root. The
// Root confines every path resolution to that directory, so neither ".." nor a symlink the bot
// planted can reach the rest of the host. On top of that, symlinks are never followed at all:
// a path whose components (or, for reads and writes, whose target) is a symlink is refused, and
// opened files are checked against what was looked up so a swap in between is detected.
//
// rel is a cleaned relative path from splitFilesPath ("." for the root itself); disp is the
// path as the panel gave it, used in error messages instead of host paths.

var errNotRegular = errors.New("not a regular file")

// lstatPath looks up rel without following symlinks anywhere along it. Intermediate symlinks
// and non-directories are errors; the final component is returned as is (it may be a link).
func lstatPath(root *os.Root, rel string) (fs.FileInfo, error) {
	if rel == "." {
		return root.Lstat(".")
	}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		fi, err := root.Lstat(strings.Join(parts[:i], "/"))
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, errSymlink
		}
		if !fi.IsDir() {
			return nil, syscall.ENOTDIR
		}
	}
	return root.Lstat(rel)
}

func isLink(fi fs.FileInfo) bool { return fi.Mode()&fs.ModeSymlink != 0 }

// filesErr turns err into a message for the panel: host paths are dropped, the panel's path
// is kept, and the underlying errno stays reachable with errors.Is.
func filesErr(disp string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidPath) {
		return ErrInvalidPath
	}
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		err = pe.Err
	case errors.As(err, &le):
		err = le.Err
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: no such file or directory", disp)
	}
	return fmt.Errorf("%s: %w", disp, err)
}

func entryOf(fi fs.FileInfo) proto.FileEntry {
	return proto.FileEntry{
		Name:    fi.Name(),
		Dir:     fi.IsDir(),
		Link:    isLink(fi),
		Size:    fi.Size(),
		Mode:    uint32(fi.Mode().Perm()),
		ModTime: fi.ModTime().UTC(),
	}
}

// listDir lists the directory rel: directories first, then everything else, each by name,
// cut after maxListEntries.
func listDir(root *os.Root, rel, disp string) (proto.FilesListing, error) {
	fi, err := lstatPath(root, rel)
	if err != nil {
		return proto.FilesListing{}, filesErr(disp, err)
	}
	if isLink(fi) {
		return proto.FilesListing{}, filesErr(disp, errSymlink)
	}
	if !fi.IsDir() {
		return proto.FilesListing{}, filesErr(disp, syscall.ENOTDIR)
	}
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return proto.FilesListing{}, filesErr(disp, err)
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !os.SameFile(st, fi) {
		return proto.FilesListing{}, filesErr(disp, errors.New("changed while listing, try again"))
	}
	des, err := f.ReadDir(-1)
	if err != nil {
		return proto.FilesListing{}, filesErr(disp, err)
	}
	// Order by the directory entry types first so only the entries kept are stat'ed.
	order := make([]proto.FileEntry, len(des))
	for i, de := range des {
		order[i] = proto.FileEntry{Name: de.Name(), Dir: de.IsDir()}
	}
	sortEntries(order)
	if len(order) > maxListEntries {
		order = order[:maxListEntries]
	}
	l := proto.FilesListing{Path: disp, Entries: make([]proto.FileEntry, 0, len(order))}
	for _, e := range order {
		efi, err := root.Lstat(path.Join(rel, e.Name))
		if err != nil {
			continue // gone meanwhile
		}
		l.Entries = append(l.Entries, entryOf(efi))
	}
	sortEntries(l.Entries)
	return l, nil
}

// readFile reads the regular file rel. Files over MaxFileRead are reported Truncated and
// binary files Binary, both without content.
func readFile(root *os.Root, rel, disp string) (proto.FileContent, error) {
	if rel == "." {
		return proto.FileContent{}, filesErr(disp, syscall.EISDIR)
	}
	fi, err := lstatPath(root, rel)
	if err != nil {
		return proto.FileContent{}, filesErr(disp, err)
	}
	switch {
	case isLink(fi):
		return proto.FileContent{}, filesErr(disp, errSymlink)
	case fi.IsDir():
		return proto.FileContent{}, filesErr(disp, syscall.EISDIR)
	case !fi.Mode().IsRegular():
		return proto.FileContent{}, filesErr(disp, errNotRegular)
	}
	out := proto.FileContent{Path: disp, Size: fi.Size(), ModTime: fi.ModTime().UTC()}
	if fi.Size() > proto.MaxFileRead {
		out.Truncated = true
		return out, nil
	}
	// O_NONBLOCK: if the file was swapped for a FIFO after the lookup, open must not hang.
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return proto.FileContent{}, filesErr(disp, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return proto.FileContent{}, filesErr(disp, err)
	}
	if !st.Mode().IsRegular() || !os.SameFile(st, fi) {
		return proto.FileContent{}, filesErr(disp, errors.New("changed while reading, try again"))
	}
	b, err := io.ReadAll(io.LimitReader(f, proto.MaxFileRead+1))
	if err != nil {
		return proto.FileContent{}, filesErr(disp, err)
	}
	out.Size, out.ModTime = st.Size(), st.ModTime().UTC()
	switch {
	case len(b) > proto.MaxFileRead:
		out.Truncated = true
	case isBinary(b):
		out.Binary = true
	default:
		out.Content = b
	}
	return out, nil
}

// writeFile replaces (or creates) the regular file rel with content, atomically: a temp file
// in the same directory is written, synced and renamed over the target. The parent directory
// must exist. New files get mode 0644, overwritten ones keep their permission bits; both end
// up owned by uid:uid.
func writeFile(root *os.Root, rel, disp string, content []byte, uid int) (err error) {
	if len(content) > proto.MaxFileWrite {
		return filesErr(disp, fmt.Errorf("%w (limit %d KB)", errTooLarge, proto.MaxFileWrite>>10))
	}
	if rel == "." {
		return filesErr(disp, syscall.EISDIR)
	}
	dir, base := path.Split(rel)
	dir = path.Clean(dir)
	pfi, err := lstatPath(root, dir)
	if err != nil {
		return filesErr(path.Dir(disp), err)
	}
	if isLink(pfi) {
		return filesErr(path.Dir(disp), errSymlink)
	}
	if !pfi.IsDir() {
		return filesErr(path.Dir(disp), syscall.ENOTDIR)
	}
	mode := fs.FileMode(0o644)
	fi, err := root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return filesErr(disp, err)
	case isLink(fi):
		return filesErr(disp, errSymlink)
	case fi.IsDir():
		return filesErr(disp, syscall.EISDIR)
	case !fi.Mode().IsRegular():
		return filesErr(disp, errNotRegular)
	default:
		mode = fi.Mode().Perm()
	}

	var rnd [8]byte
	rand.Read(rnd[:])
	tmp := path.Join(dir, "."+base+".mechon-"+hex.EncodeToString(rnd[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return filesErr(disp, err)
	}
	defer func() {
		if err != nil {
			f.Close()
			root.Remove(tmp)
		}
	}()
	// fd-based: they act on the file we created, whatever happens to its name meanwhile.
	if err = f.Chown(uid, uid); err != nil {
		return filesErr(disp, err)
	}
	if _, err = f.Write(content); err != nil {
		return filesErr(disp, err)
	}
	// Sync so a full disk (delayed allocation) is reported here rather than lost.
	if err = f.Sync(); err != nil {
		return filesErr(disp, err)
	}
	if err = f.Chmod(mode); err != nil {
		return filesErr(disp, err)
	}
	if err = f.Close(); err != nil {
		return filesErr(disp, err)
	}
	// Rename replaces a name in a directory and never follows a symlink at the target.
	if err = root.Rename(tmp, rel); err != nil {
		return filesErr(disp, err)
	}
	return nil
}

// makeDir creates the directory rel (mode 0755, owned by uid:uid). Its parent must exist.
func makeDir(root *os.Root, rel, disp string, uid int) error {
	if rel == "." {
		return filesErr(disp, fs.ErrExist)
	}
	pfi, err := lstatPath(root, path.Dir(rel))
	if err != nil {
		return filesErr(path.Dir(disp), err)
	}
	if isLink(pfi) {
		return filesErr(path.Dir(disp), errSymlink)
	}
	if !pfi.IsDir() {
		return filesErr(path.Dir(disp), syscall.ENOTDIR)
	}
	if err := root.Mkdir(rel, 0o700); err != nil {
		return filesErr(disp, err)
	}
	// Lchown/Chmod act on the name: if the bot swapped the new directory for a symlink, the
	// Root still keeps the effect inside the bot's own files.
	if err := root.Lchown(rel, uid, uid); err != nil {
		return filesErr(disp, err)
	}
	if err := root.Chmod(rel, 0o755); err != nil {
		return filesErr(disp, err)
	}
	return nil
}

// deleteFile removes rel: a file, a symlink (the link itself), or a directory recursively.
// The root itself cannot be deleted.
func deleteFile(root *os.Root, rel, disp string) error {
	if rel == "." {
		return fmt.Errorf("%s: cannot delete a root folder", disp)
	}
	fi, err := lstatPath(root, rel)
	if err != nil {
		return filesErr(disp, err)
	}
	if fi.IsDir() {
		// Root.RemoveAll works with openat/unlinkat under the Root and never follows links.
		err = root.RemoveAll(rel)
	} else {
		err = root.Remove(rel)
	}
	return filesErr(disp, err)
}
