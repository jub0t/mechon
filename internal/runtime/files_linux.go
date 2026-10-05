//go:build linux

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jub0t/mechon/internal/proto"
)

// File manager for the Docker runtime. Everything works on the bot's loop-mounted volume
// from the agent's side; nothing runs inside the bot's container.

// openFilesRoot validates p and opens the bot's app/ or data/ directory as an os.Root.
func (d *Docker) openFilesRoot(botID, p string) (*os.Root, string, error) {
	top, rel, err := splitFilesPath(p)
	if err != nil {
		return nil, "", err
	}
	if err := d.volumeReady(botID); err != nil {
		return nil, "", err
	}
	vol, err := os.OpenRoot(d.mountPath(botID))
	if err != nil {
		return nil, "", fmt.Errorf("open volume: %w", unwrapPath(err))
	}
	defer vol.Close()
	// The volume root is root-owned, so the bot cannot replace app/ or data/; check anyway.
	fi, err := vol.Lstat(top)
	if err != nil {
		return nil, "", filesErr(top, err)
	}
	if !fi.IsDir() || isLink(fi) {
		return nil, "", fmt.Errorf("%s is not a directory", top)
	}
	r, err := vol.OpenRoot(top)
	if err != nil {
		return nil, "", filesErr(top, err)
	}
	return r, rel, nil
}

// volumeReady checks that the bot has a disk image and that it is mounted, so nothing is
// ever read from or written to the bare mount point on the host's own disk.
func (d *Docker) volumeReady(botID string) error {
	if !validBotID(botID) {
		return fmt.Errorf("invalid bot id %q", botID)
	}
	if _, err := os.Stat(d.imagePath(botID)); errors.Is(err, fs.ErrNotExist) {
		return ErrNoFiles
	} else if err != nil {
		return fmt.Errorf("volume: %w", unwrapPath(err))
	}
	src, err := mountSource(d.mountPath(botID))
	if err != nil {
		return fmt.Errorf("volume: %w", err)
	}
	if src == "" {
		return errors.New("the bot's volume is not mounted")
	}
	return nil
}

func unwrapPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func (d *Docker) ListFiles(ctx context.Context, botID, p string) (proto.FilesListing, error) {
	if p == "" {
		if err := d.volumeReady(botID); err != nil {
			return proto.FilesListing{}, err
		}
		return rootListing(), nil
	}
	r, rel, err := d.openFilesRoot(botID, p)
	if err != nil {
		return proto.FilesListing{}, err
	}
	defer r.Close()
	return listDir(r, rel, p)
}

func (d *Docker) ReadFile(ctx context.Context, botID, p string) (proto.FileContent, error) {
	r, rel, err := d.openFilesRoot(botID, p)
	if err != nil {
		return proto.FileContent{}, err
	}
	defer r.Close()
	return readFile(r, rel, p)
}

func (d *Docker) WriteFile(ctx context.Context, botID, p string, content []byte, uid int) error {
	if uid < 1000 {
		return fmt.Errorf("invalid uid %d", uid)
	}
	r, rel, err := d.openFilesRoot(botID, p)
	if err != nil {
		return err
	}
	defer r.Close()
	return writeFile(r, rel, p, content, uid)
}

func (d *Docker) DeleteFile(ctx context.Context, botID, p string) error {
	r, rel, err := d.openFilesRoot(botID, p)
	if err != nil {
		return err
	}
	defer r.Close()
	return deleteFile(r, rel, p)
}

func (d *Docker) MakeDir(ctx context.Context, botID, p string, uid int) error {
	if uid < 1000 {
		return fmt.Errorf("invalid uid %d", uid)
	}
	r, rel, err := d.openFilesRoot(botID, p)
	if err != nil {
		return err
	}
	defer r.Close()
	return makeDir(r, rel, p, uid)
}
