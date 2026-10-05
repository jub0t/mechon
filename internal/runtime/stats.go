//go:build linux

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// Stats are read straight from cgroup v2 files and /proc: far cheaper than Docker's stats API.

type liveInfo struct {
	pid       int
	cgroupDir string
}

type cpuSample struct {
	usageUsec int64
	at        time.Time
}

func (d *Docker) Stats(ctx context.Context) ([]proto.BotStats, error) {
	cs, err := d.api.listContainers(ctx, labelBot)
	if err != nil {
		return nil, err
	}
	out := make([]proto.BotStats, 0, len(cs))
	for _, c := range cs {
		botID := c.Labels[labelBot]
		if c.State != "running" || !validBotID(botID) {
			continue
		}
		li, err := d.liveInfo(ctx, c.ID)
		if err != nil {
			continue
		}
		s := proto.BotStats{BotID: botID, DiskBytes: d.volumeUsed(botID)}
		s.MemoryBytes, _ = readInt(filepath.Join(li.cgroupDir, "memory.current"))
		if usage, err := readCPUUsage(li.cgroupDir); err == nil {
			now := time.Now()
			d.mu.Lock()
			prev, ok := d.cpuPrev[c.ID]
			d.cpuPrev[c.ID] = cpuSample{usage, now}
			d.mu.Unlock()
			if ok && now.After(prev.at) && usage >= prev.usageUsec {
				cpus := cgroupCPUs(li.cgroupDir)
				elapsed := float64(now.Sub(prev.at).Microseconds())
				s.CPUPercent = min(100, float64(usage-prev.usageUsec)/(elapsed*cpus)*100)
			}
		}
		s.NetRxBytes, s.NetTxBytes = readNetDev(li.pid)
		out = append(out, s)
	}
	return out, nil
}

func (d *Docker) liveInfo(ctx context.Context, containerID string) (*liveInfo, error) {
	d.mu.Lock()
	li := d.live[containerID]
	d.mu.Unlock()
	if li != nil {
		return li, nil
	}
	c, err := d.api.inspectContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if !c.State.Running || c.State.Pid == 0 {
		return nil, fmt.Errorf("not running")
	}
	dir, err := cgroupDir(c.State.Pid, c.ID)
	if err != nil {
		return nil, err
	}
	li = &liveInfo{pid: c.State.Pid, cgroupDir: dir}
	d.mu.Lock()
	d.live[containerID] = li
	d.mu.Unlock()
	return li, nil
}

// cgroupDir finds the container's cgroup v2 directory: from /proc/<pid>/cgroup (works for any
// cgroup driver), falling back to the systemd and cgroupfs driver layouts.
func cgroupDir(pid int, id string) (string, error) {
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if p, ok := strings.CutPrefix(line, "0::"); ok {
				dir := filepath.Join("/sys/fs/cgroup", p)
				if _, err := os.Stat(filepath.Join(dir, "cpu.stat")); err == nil {
					return dir, nil
				}
			}
		}
	}
	for _, dir := range []string{
		"/sys/fs/cgroup/system.slice/docker-" + id + ".scope",
		"/sys/fs/cgroup/docker/" + id,
	} {
		if _, err := os.Stat(filepath.Join(dir, "cpu.stat")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("cgroup of container %s not found", id)
}

func readInt(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}

func readCPUUsage(dir string) (int64, error) {
	b, err := os.ReadFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, fmt.Errorf("usage_usec missing")
}

// cgroupCPUs is the CPU limit in cores from cpu.max ("max 100000" means unlimited: all cores).
func cgroupCPUs(dir string) float64 {
	b, err := os.ReadFile(filepath.Join(dir, "cpu.max"))
	if err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 && f[0] != "max" {
			q, err1 := strconv.ParseFloat(f[0], 64)
			p, err2 := strconv.ParseFloat(f[1], 64)
			if err1 == nil && err2 == nil && p > 0 && q > 0 {
				return q / p
			}
		}
	}
	return float64(numCPU())
}

// readNetDev sums rx/tx bytes of every non-loopback interface in the process's netns.
func readNetDev(pid int) (rx, tx int64) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/net/dev", pid))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseInt(fields[0], 10, 64)
		t, _ := strconv.ParseInt(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

// ---------- Host ----------

func (h *HostSampler) Sample() proto.NodeStats {
	var s proto.NodeStats
	if busy, all, err := readProcStat(); err == nil {
		if h.prevAll > 0 && all > h.prevAll {
			s.CPUPercent = float64(busy-h.prevBusy) / float64(all-h.prevAll) * 100
		}
		h.prevBusy, h.prevAll = busy, all
	}
	if total, avail, err := readMeminfo(); err == nil {
		s.MemoryBytes = total - avail
	}
	if _, used, err := diskUsage(h.DataDir); err == nil {
		s.DiskBytes = used
	}
	return s
}

func readProcStat() (busy, all uint64, err error) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, fmt.Errorf("bad /proc/stat")
	}
	var idle uint64
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		if i == 8 || i == 9 { // guest, guest_nice are already in user/nice
			continue
		}
		all += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	return all - idle, all, nil
}

func readMeminfo() (total, avail int64, err error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	return total, avail, nil
}

func diskUsage(path string) (total, used int64, err error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, err
	}
	return int64(s.Blocks) * int64(s.Bsize), int64(s.Blocks-s.Bfree) * int64(s.Bsize), nil
}

func numCPU() int {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 1
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return max(n, 1)
}

func (d *Docker) HostInfo(ctx context.Context) (HostInfo, error) {
	var h HostInfo
	v, err := d.api.version(ctx)
	if err != nil {
		return h, err
	}
	info, err := d.api.info(ctx)
	if err != nil {
		return h, err
	}
	_, runsc := info.Runtimes["runsc"]
	d.runsc.Store(runsc)
	h.DockerVersion = v.Version
	h.Kernel = v.KernelVersion
	h.Runsc = runsc
	h.Hostname, _ = os.Hostname()
	_, err = os.Stat("/sys/fs/cgroup/cgroup.controllers")
	h.CgroupV2 = err == nil
	h.CPUs = numCPU()
	if total, _, err := readMeminfo(); err == nil {
		h.MemoryBytes = total
	}
	if total, _, err := diskUsage(d.dataDir); err == nil {
		h.DiskBytes = total
	}
	return h, nil
}
