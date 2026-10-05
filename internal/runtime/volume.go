//go:build linux

package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Volume layout: DATA_DIR/volumes/<bot>.img (sparse ext4) loop-mounted at DATA_DIR/mounts/<bot>.
//
//	app/      current code, bind-mounted rw at /home/container   (owned by the bot UID)
//	data/     persistent data, bind-mounted rw at /data          (owned by the bot UID)
//	.mechon/  agent bookkeeping, never mounted into a container  (root, 0700)
//	  deploy  id of the deploy in app/
//	  next/   deploy being installed
//	  prev/   previous app/ (one generation)

func (d *Docker) imagePath(botID string) string {
	return filepath.Join(d.dataDir, "volumes", botID+".img")
}
func (d *Docker) mountPath(botID string) string { return filepath.Join(d.dataDir, "mounts", botID) }
func (d *Docker) appDir(botID string) string    { return filepath.Join(d.mountPath(botID), "app") }
func (d *Docker) dataPath(botID string) string  { return filepath.Join(d.mountPath(botID), "data") }
func (d *Docker) metaDir(botID string) string   { return filepath.Join(d.mountPath(botID), ".mechon") }

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// mountSource returns the device mounted at dir ("" if dir is not a mount point).
func mountSource(dir string) (string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		// id parent major:minor root mountpoint opts ... - fstype source superopts
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || unescapeMount(fields[4]) != dir {
			continue
		}
		for i, f := range fields {
			if f == "-" && i+2 < len(fields) {
				return fields[i+2], nil
			}
		}
		return "?", nil
	}
	return "", sc.Err()
}

func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// ensureVolume creates (if needed), mounts and grows the bot's disk image and lays out its
// directories. Shrinking is refused: the image keeps its size and errShrink is returned.
func (d *Docker) ensureVolume(botID string, uid, diskMB int) error {
	img, mnt := d.imagePath(botID), d.mountPath(botID)
	want := int64(diskMB) * mib

	st, err := os.Stat(img)
	switch {
	case errors.Is(err, os.ErrNotExist):
		f, err := os.OpenFile(img, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		f.Close()
		if err := os.Truncate(img, want); err != nil {
			os.Remove(img)
			return err
		}
		// -m 0: no reserved blocks; the whole quota belongs to the bot.
		if err := run("mkfs.ext4", "-q", "-F", "-m", "0", "-E", "lazy_itable_init=1,lazy_journal_init=1", img); err != nil {
			os.Remove(img)
			return err
		}
	case err != nil:
		return err
	}

	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return err
	}
	src, err := mountSource(mnt)
	if err != nil {
		return err
	}
	if src == "" {
		if err := run("mount", "-o", "loop,nodev,nosuid", img, mnt); err != nil {
			return err
		}
		if src, err = mountSource(mnt); err != nil {
			return err
		}
	}

	var shrinkErr error
	if st != nil {
		switch {
		case st.Size() < want:
			if err := growVolume(img, src, want); err != nil {
				return fmt.Errorf("grow volume: %w", err)
			}
		case st.Size() > want:
			shrinkErr = fmt.Errorf("%w: volume is %d MB, spec asks for %d MB", errShrink, st.Size()/mib, diskMB)
		}
	}

	// The volume root stays root-owned so the bot cannot rename app/ or data/ or reach .mechon.
	if err := os.Chmod(mnt, 0o755); err != nil {
		return err
	}
	for _, dir := range []string{"app", "data"} {
		p := filepath.Join(mnt, dir)
		if err := os.Mkdir(p, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := os.Lchown(p, uid, uid); err != nil {
			return err
		}
	}
	if err := os.Mkdir(filepath.Join(mnt, ".mechon"), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return shrinkErr
}

var errShrink = errors.New("refusing to shrink volume")

// growVolume grows a mounted image online: extend the file, refresh the loop device, resize ext4.
func growVolume(img, dev string, size int64) error {
	if err := os.Truncate(img, size); err != nil {
		return err
	}
	if !strings.HasPrefix(dev, "/dev/loop") {
		return fmt.Errorf("volume is mounted from %q, not a loop device", dev)
	}
	if err := run("losetup", "-c", dev); err != nil {
		return err
	}
	return run("resize2fs", dev)
}

// unmountVolume unmounts the bot's volume if it is mounted. The loop device is released
// automatically (mount -o loop sets autoclear).
func (d *Docker) unmountVolume(botID string) error {
	mnt := d.mountPath(botID)
	src, err := mountSource(mnt)
	if err != nil || src == "" {
		return err
	}
	if err := syscall.Unmount(mnt, 0); err != nil {
		// Something still holds it (e.g. a container that has not fully gone). Detach lazily so
		// the image can be deleted; the kernel frees it when the last user goes.
		if err2 := syscall.Unmount(mnt, syscall.MNT_DETACH); err2 != nil {
			return fmt.Errorf("unmount %s: %v", mnt, err)
		}
	}
	return nil
}

// volumeUsed returns the bytes used inside the bot's mounted volume.
func (d *Docker) volumeUsed(botID string) int64 {
	var s syscall.Statfs_t
	if syscall.Statfs(d.mountPath(botID), &s) != nil {
		return 0
	}
	return int64(s.Blocks-s.Bfree) * int64(s.Bsize)
}

func (d *Docker) readDeployID(botID string) string {
	b, err := os.ReadFile(filepath.Join(d.metaDir(botID), "deploy"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (d *Docker) writeDeployID(botID, deployID string) error {
	p := filepath.Join(d.metaDir(botID), "deploy")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(deployID+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// volumeIDs lists bots that have a disk image.
func (d *Docker) volumeIDs() ([]string, error) {
	ents, err := os.ReadDir(filepath.Join(d.dataDir, "volumes"))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		if name, ok := strings.CutSuffix(e.Name(), ".img"); ok && validBotID(name) {
			ids = append(ids, name)
		}
	}
	return ids, nil
}

// remountAll mounts every existing image (after an agent or node restart).
func (d *Docker) remountAll() error {
	ids, err := d.volumeIDs()
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		mnt := d.mountPath(id)
		if err := os.MkdirAll(mnt, 0o700); err != nil {
			errs = append(errs, err)
			continue
		}
		src, err := mountSource(mnt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if src == "" {
			if err := run("mount", "-o", "loop,nodev,nosuid", d.imagePath(id), mnt); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
