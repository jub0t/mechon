//go:build linux

package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// TestIsolation attacks one bot from inside another. Every attack must fail, and each network
// check has a positive control from outside Mechon's rules so a pass is never vacuous.
func TestIsolation(t *testing.T) {
	e := newTestEnv(t)
	d := e.d
	events, err := d.Events(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	target := newTargetNetns(t) // 10.250.0.2 and 169.254.169.254, reachable by forwarding

	// Bot A: the attacker. Small disk so filling it is quick.
	a := testSpecBase("iso-a", 200001)
	a.Limits = proto.Limits{MemoryMB: 256, CPUMillicores: 500, DiskMB: 32, Pids: 64}
	a.Env["DISCORD_TOKEN"] = "tokA"
	e.withCode(&a, "dep-a", map[string]string{"run.sh": "echo A up; exec sleep 3600"})
	a.Template.Start = "sh run.sh"
	e.apply(a)

	// Bot B: the victim, serving HTTP on 8080 and holding secrets.
	b := testSpecBase("iso-b", 200002)
	b.Template.Image = "node:22-bookworm-slim"
	b.Template.Start = "node server.js"
	b.Env["DISCORD_TOKEN"] = "tokB-secret"
	e.withCode(&b, "dep-b", map[string]string{
		"server.js": `require('fs').writeFileSync('/data/secret-b-data.txt', 'SECRET_B_DATA');
require('http').createServer((q, s) => s.end('B-OK')).listen(8080, () => console.log('B listening'));`,
		"secret-b.txt": "SECRET_B_FILE",
	})
	e.apply(b)
	waitFor(t, "B listening", 30*time.Second, func() bool { return strings.Contains(logText(t, d, b.BotID), "B listening") })
	bIP, _ := sh(t, "docker inspect -f '{{.NetworkSettings.Networks.mechon0.IPAddress}}' "+containerName(b.BotID))
	gw, _ := sh(t, "docker network inspect -f '{{(index .IPAM.Config 0).Gateway}}' mechon0")
	hostIP, _ := sh(t, "ip -4 -o addr show scope global | awk '$2 !~ /^(docker|mechon|br-|veth|mtgt)/ {print $4}' | cut -d/ -f1 | head -1")
	t.Logf("B=%s mechon0 gateway=%s host=%s", bIP, gw, hostIP)

	mustFail := func(t *testing.T, what, script string) {
		t.Helper()
		out, err := inBot(t, a.BotID, script)
		if err == nil {
			t.Errorf("%s: succeeded from bot A, must fail. output: %s", what, out)
		} else {
			t.Logf("%s: blocked (%s)", what, firstLine(out))
		}
	}
	mustWork := func(t *testing.T, what, script string) string {
		t.Helper()
		out, err := inBot(t, a.BotID, script)
		if err != nil {
			t.Fatalf("%s: control failed: %v: %s", what, err, out)
		}
		return out
	}

	t.Run("read other bot's files and env", func(t *testing.T) {
		out, _ := inBot(t, a.BotID, `
			cat /proc/*/root/home/container/secret-b.txt 2>/dev/null
			find / -path /proc -prune -o -path /sys -prune -o -name 'secret-b*' -print 2>/dev/null
			cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep DISCORD_TOKEN
			ls /var/lib 2>/dev/null; ps -o pid,user,args`)
		for _, s := range []string{"SECRET_B", "secret-b", "tokB", "node server.js", "mechon"} {
			if strings.Contains(out, s) {
				t.Errorf("bot A can see %q:\n%s", s, out)
			}
		}
		if !strings.Contains(out, "DISCORD_TOKEN=tokA") {
			t.Errorf("control: A should see its own env:\n%s", out)
		}
		// Defence in depth: even with a path to B's files, A's UID cannot read them.
		for _, p := range []string{filepath.Join(d.appDir(b.BotID), "secret-b.txt"), filepath.Join(d.dataPath(b.BotID), "secret-b-data.txt")} {
			if out, err := sh(t, "setpriv --reuid 200001 --regid 200001 --clear-groups cat "+p); err == nil {
				t.Errorf("UID of A can read %s: %s", p, out)
			}
			if out, err := sh(t, "setpriv --reuid 200002 --regid 200002 --clear-groups cat "+p); err == nil {
				t.Logf("note: B's own UID can read %s only via the host path because mounts/ is root-only: %s", p, out)
			}
		}
	})

	t.Run("write outside writable paths", func(t *testing.T) {
		for _, p := range []string{"/", "/etc", "/usr", "/usr/local/bin", "/var", "/root", "/home", "/opt", "/bin", "/dev"} {
			mustFail(t, "write "+p, "touch "+p+"/pwned")
		}
		mustWork(t, "write allowed paths", "touch /home/container/ok /data/ok /tmp/ok")
	})

	t.Run("exec from tmp", func(t *testing.T) {
		mustFail(t, "exec /tmp", "cp /bin/busybox /tmp/busybox && chmod +x /tmp/busybox && /tmp/busybox true")
		mustFail(t, "exec /dev/shm", "cp /bin/busybox /dev/shm/busybox && chmod +x /dev/shm/busybox && /dev/shm/busybox true")
		mustWork(t, "exec /home/container", "cp /bin/busybox /home/container/busybox && /home/container/busybox true && rm /home/container/busybox")
	})

	t.Run("reach other bot", func(t *testing.T) {
		if out, err := sh(t, "wget -T 3 -qO- http://"+bIP+":8080/ || curl -s -m 3 http://"+bIP+":8080/"); err != nil || !strings.Contains(out, "B-OK") {
			t.Fatalf("control: host cannot reach B: %v %s", err, out)
		}
		mustFail(t, "HTTP to bot B", "wget -T 3 -qO- http://"+bIP+":8080/")
	})

	t.Run("reach host", func(t *testing.T) {
		if out, _ := sh(t, "docker run --rm alpine:3.20 sh -c 'echo | nc -w 3 "+hostIP+" 22'"); !strings.Contains(out, "SSH-") {
			t.Fatalf("control: a default-bridge container cannot reach the host's sshd: %s", out)
		}
		for _, ip := range []string{gw, hostIP, "172.17.0.1"} {
			mustFail(t, "host sshd at "+ip, "echo | nc -w 3 "+ip+" 22 | grep SSH-")
		}
	})

	t.Run("reach private network and metadata", func(t *testing.T) {
		for _, ip := range []string{target.private, target.metadata} {
			if out, err := sh(t, "docker run --rm alpine:3.20 wget -T 3 -qO- http://"+ip+":8080/"); err != nil || !strings.Contains(out, "TARGET-OK") {
				t.Fatalf("control: default-bridge container cannot reach %s: %v %s", ip, err, out)
			}
			mustFail(t, "HTTP to "+ip, "wget -T 3 -qO- http://"+ip+":8080/")
		}
	})

	t.Run("public internet works", func(t *testing.T) {
		out := mustWork(t, "resolve + HTTPS to Discord", "nslookup discord.com >/dev/null && wget -T 15 -qO- https://discord.com/api/v10/gateway")
		if !strings.Contains(out, "gateway.discord.gg") {
			t.Fatalf("unexpected answer: %s", out)
		}
		mustWork(t, "TCP to discord.com:443", "echo | nc -w 5 discord.com 443; true")
	})

	t.Run("privileges", func(t *testing.T) {
		out := mustWork(t, "status", "grep -E '^(CapInh|CapPrm|CapEff|CapBnd|CapAmb|NoNewPrivs|Seccomp):' /proc/self/status; id")
		for _, want := range []string{"CapPrm:\t0000000000000000", "CapEff:\t0000000000000000", "CapBnd:\t0000000000000000", "NoNewPrivs:\t1", "Seccomp:\t2", "uid=200001"} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q in:\n%s", want, out)
			}
		}
		mustFail(t, "chown to root", "touch /home/container/f && chown 0:0 /home/container/f")
		mustFail(t, "mknod", "mknod /home/container/mem c 1 1")
		mustFail(t, "mount", "mount -t tmpfs none /home/container")
		mustFail(t, "user namespace", "unshare -r true")
		mustFail(t, "setuid via su", "su -c id root </dev/null")

		// A root-owned setuid binary inside the bot's volume (as if it got there somehow) must not
		// elevate: the volume is mounted nosuid and the bot runs with no-new-privileges.
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		helper := filepath.Join(d.appDir(a.BotID), "suidhelper")
		copyFile(t, self, helper)
		os.Chown(helper, 0, 0)
		if err := os.Chmod(helper, 0o755|os.ModeSetuid); err != nil {
			t.Fatal(err)
		}
		// Control: the same setuid binary does elevate outside Mechon.
		ctrlDir, _ := os.MkdirTemp("/var/tmp", "suidctl")
		defer os.RemoveAll(ctrlDir)
		os.Chmod(ctrlDir, 0o755)
		ctrl := filepath.Join(ctrlDir, "suidhelper")
		copyFile(t, self, ctrl)
		os.Chown(ctrl, 0, 0)
		if err := os.Chmod(ctrl, 0o755|os.ModeSetuid); err != nil {
			t.Fatal(err)
		}
		if out, _ := sh(t, "MECHON_EUID_HELPER=1 setpriv --reuid 200001 --regid 200001 --clear-groups "+ctrl); !strings.Contains(out, "euid=0") {
			t.Fatalf("control: setuid helper does not elevate on the host: %s", out)
		}
		out = mustWork(t, "run setuid helper", "MECHON_EUID_HELPER=1 /home/container/suidhelper")
		if !strings.Contains(out, "euid=200001") {
			t.Errorf("setuid binary elevated inside the bot: %s", out)
		}
		os.Remove(helper)
	})

	t.Run("disk quota", func(t *testing.T) {
		before := hostFree(t, d.dataDir)
		mustFail(t, "fill /home/container", "dd if=/dev/zero of=/home/container/fill bs=1M count=100")
		mustFail(t, "fill /data (same image)", "dd if=/dev/zero of=/data/fill2 bs=1M count=100")
		mustFail(t, "fill /tmp beyond 64 MB", "dd if=/dev/zero of=/tmp/fill bs=1M count=100")
		if used := before - hostFree(t, d.dataDir); used > 40*mib {
			t.Errorf("host lost %d MB to a 32 MB bot", used/mib)
		}
		// Other bots and the host are unaffected.
		if out, err := inBot(t, b.BotID, "echo ok > /home/container/after && cat /home/container/after"); err != nil || out != "ok" {
			t.Errorf("bot B cannot write after A filled its disk: %v %s", err, out)
		}
		mustWork(t, "A cleans up", "rm -f /home/container/fill /data/fill2 /tmp/fill")
		stats, _ := d.Stats(e.ctx)
		t.Logf("stats after fill: %+v", stats)
	})

	t.Run("fork bomb", func(t *testing.T) {
		bomb := testSpecBase("iso-bomb", 200003)
		bomb.Limits.Pids = 64
		// Fork until the kernel refuses, then keep the container alive so the cap is observable.
		e.withCode(&bomb, "dep-bomb", map[string]string{"run.sh": "( while :; do sleep 1000 & done ) 2>/dev/null; echo fork refused; exec sleep 3600"})
		bomb.Template.Start = "sh run.sh"
		e.apply(bomb)
		time.Sleep(3 * time.Second)
		li, err := d.liveInfo(e.ctx, mustContainerID(t, d, bomb.BotID))
		if err == nil {
			cur, _ := readInt(filepath.Join(li.cgroupDir, "pids.current"))
			maxs, _ := os.ReadFile(filepath.Join(li.cgroupDir, "pids.max"))
			evs, _ := os.ReadFile(filepath.Join(li.cgroupDir, "pids.events"))
			t.Logf("bomb: pids.current=%d pids.max=%s pids.events=%q", cur, strings.TrimSpace(string(maxs)), strings.TrimSpace(string(evs)))
			if cur > 64 || cur < 60 || strings.TrimSpace(string(maxs)) != "64" || strings.Contains(string(evs), "max 0") {
				t.Errorf("fork bomb not capped at 64: current=%d max=%s events=%s", cur, maxs, evs)
			}
		} else {
			t.Logf("bomb container already exited: %v", err)
		}
		if out, err := inBot(t, b.BotID, "echo alive"); err != nil || out != "alive" {
			t.Errorf("bot B affected by A's fork bomb: %v %s", err, out)
		}
		if _, err := sh(t, "true"); err != nil {
			t.Errorf("host cannot fork: %v", err)
		}
		if !strings.Contains(logText(t, d, bomb.BotID), "fork refused") {
			t.Errorf("bomb never hit the limit")
		}
		if err := d.Remove(e.ctx, bomb.BotID); err != nil {
			t.Errorf("remove bomb: %v", err)
		}
		drain(events)
	})

	t.Run("memory limit", func(t *testing.T) {
		hog := testSpecBase("iso-hog", 200004)
		hog.Limits.MemoryMB = 32
		e.withCode(&hog, "dep-hog", map[string]string{"run.sh": "echo hogging; x=aaaaaaaaaaaaaaaa; while true; do x=$x$x; done"})
		hog.Template.Start = "sh run.sh"
		if _, err := d.Apply(e.ctx, hog); err != nil {
			t.Logf("apply hog: %v", err) // it may die before Apply looks
		}
		var ev Event
		select {
		case ev = <-events:
		case <-time.After(60 * time.Second):
			t.Fatal("hog was not killed")
		}
		if ev.BotID != hog.BotID || !ev.OOMKilled {
			t.Fatalf("want OOM kill of hog, got %+v", ev)
		}
		t.Logf("hog: exit %d, OOMKilled=%v", ev.ExitCode, ev.OOMKilled)
		if out, err := inBot(t, b.BotID, "echo alive"); err != nil || out != "alive" {
			t.Errorf("bot B affected by A's OOM: %v %s", err, out)
		}
		d.Remove(e.ctx, hog.BotID)
	})

	t.Run("signal other bots", func(t *testing.T) {
		inBot(t, a.BotID, "kill -9 -1 2>/dev/null; true")
		time.Sleep(2 * time.Second)
		if out, err := inBot(t, b.BotID, "echo alive"); err != nil || out != "alive" {
			t.Errorf("bot B died when A signalled everything: %v %s", err, out)
		}
	})
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func drain(ch <-chan Event) {
	for {
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			return
		}
	}
}

func mustContainerID(t *testing.T, d *Docker, botID string) string {
	c, err := d.api.inspectContainer(t.Context(), containerName(botID))
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func copyFile(t *testing.T, src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
}

func hostFree(t *testing.T, path string) int64 {
	out, err := sh(t, "df -B1 --output=avail "+path+" | tail -1")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	return n
}

// targetNetns is a network namespace on the node reachable only by routing (not through
// Docker), holding a private IP and the cloud metadata IP, each serving HTTP on 8080.
type targetNetns struct{ private, metadata string }

func newTargetNetns(t *testing.T) targetNetns {
	const ns = "mechontgt"
	cleanup := func() {
		sh(t, "ip netns pids "+ns+" 2>/dev/null | xargs -r kill; ip netns del "+ns+" 2>/dev/null; ip link del mtgt0 2>/dev/null; ip route del 169.254.169.254/32 2>/dev/null")
	}
	cleanup()
	script := fmt.Sprintf(`set -e
ip netns add %[1]s
ip link add mtgt0 type veth peer name mtgt1
ip link set mtgt1 netns %[1]s
ip addr add 10.250.0.1/24 dev mtgt0
ip link set mtgt0 up
ip route add 169.254.169.254/32 dev mtgt0
ip -n %[1]s addr add 10.250.0.2/24 dev mtgt1
ip -n %[1]s addr add 169.254.169.254/32 dev mtgt1
ip -n %[1]s link set mtgt1 up
ip -n %[1]s link set lo up
ip -n %[1]s route add default via 10.250.0.1
mkdir -p /tmp/mechontgt && echo TARGET-OK > /tmp/mechontgt/index.html
ip netns exec %[1]s sh -c 'cd /tmp/mechontgt && nohup python3 -m http.server 8080 >/dev/null 2>&1 &'
sleep 1`, ns)
	if out, err := sh(t, script); err != nil {
		cleanup()
		t.Fatalf("target netns: %v: %s", err, out)
	}
	t.Cleanup(cleanup)
	return targetNetns{private: "10.250.0.2", metadata: "169.254.169.254"}
}
