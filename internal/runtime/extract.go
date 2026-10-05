package runtime

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

var errUnsafeArchive = errors.New("unsafe archive")

// extractTarGz extracts r into dir, which must exist and be empty. It refuses absolute paths,
// "..", links that point outside dir, and anything that is not a directory, regular file,
// symlink or hardlink. Every write goes through os.Root, so even a crafted chain of in-tree
// symlinks cannot make a write land outside dir. Regular file bytes are capped at maxBytes.
// Everything created is owned by uid:uid; setuid/setgid/sticky bits are dropped.
func extractTarGz(r io.Reader, dir string, maxBytes int64, uid int) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("artifact is not gzip: %w", err)
	}
	defer zr.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	tr := tar.NewReader(zr)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		name, err := safeName(h.Name)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}
		mode := fs.FileMode(h.Mode) & 0o777
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return fmt.Errorf("%w: %s: %v", errUnsafeArchive, name, err)
			}
			if err := root.Chmod(name, mode|0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := mkParents(root, name, uid); err != nil {
				return err
			}
			if h.Size < 0 || total+h.Size > maxBytes {
				return fmt.Errorf("artifact expands beyond the %d MB disk quota", maxBytes/mib)
			}
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o600)
			if err != nil {
				return fmt.Errorf("%w: %s: %v", errUnsafeArchive, name, err)
			}
			n, err := io.Copy(f, io.LimitReader(tr, h.Size))
			cerr := f.Close()
			if err != nil {
				return err
			}
			if cerr != nil {
				return cerr
			}
			total += n
		case tar.TypeSymlink:
			if err := checkLinkTarget(name, h.Linkname, true); err != nil {
				return err
			}
			if err := mkParents(root, name, uid); err != nil {
				return err
			}
			if err := root.Symlink(h.Linkname, name); err != nil {
				return fmt.Errorf("%w: %s: %v", errUnsafeArchive, name, err)
			}
		case tar.TypeLink:
			target, err := safeName(h.Linkname)
			if err != nil {
				return err
			}
			if err := mkParents(root, name, uid); err != nil {
				return err
			}
			// os.Root resolves both paths inside dir; a hardlink to a directory or across the
			// root is refused by the kernel or by Root.
			if err := root.Link(target, name); err != nil {
				return fmt.Errorf("%w: hardlink %s -> %s: %v", errUnsafeArchive, name, target, err)
			}
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("%w: %s has unsupported type %q", errUnsafeArchive, name, h.Typeflag)
		}
		if err := root.Lchown(name, uid, uid); err != nil {
			return err
		}
	}
}

// safeName cleans an archive member name and rejects absolute paths and "..".
func safeName(n string) (string, error) {
	if n == "" || strings.ContainsRune(n, 0) || strings.Contains(n, `\`) {
		return "", fmt.Errorf("%w: bad name %q", errUnsafeArchive, n)
	}
	if strings.HasPrefix(n, "/") {
		return "", fmt.Errorf("%w: absolute path %q", errUnsafeArchive, n)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: path traversal %q", errUnsafeArchive, n)
		}
	}
	return path.Clean(n), nil
}

// checkLinkTarget rejects absolute link targets and targets that resolve (lexically) outside
// the extraction root.
func checkLinkTarget(name, target string, relToLink bool) error {
	if target == "" || strings.HasPrefix(target, "/") || strings.ContainsRune(target, 0) {
		return fmt.Errorf("%w: link %s -> %q points outside", errUnsafeArchive, name, target)
	}
	p := target
	if relToLink {
		p = path.Join(path.Dir(name), target)
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%w: link %s -> %q points outside", errUnsafeArchive, name, target)
	}
	return nil
}

// mkParents creates missing parent directories of name, owned by uid.
func mkParents(root *os.Root, name string, uid int) error {
	dir := path.Dir(name)
	if dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		err := root.Mkdir(p, 0o755)
		if err == nil {
			if err := root.Lchown(p, uid, uid); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s: %v", errUnsafeArchive, p, err)
		}
	}
	return nil
}
