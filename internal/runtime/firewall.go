//go:build linux

package runtime

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// Bot network: one bridge per node with inter-container traffic disabled, and egress rules that
// keep bots off private networks, cloud metadata and the host itself.
const (
	networkName = "mechon0"
	bridgeName  = "mechon0"
	chainEgress = "MECHON-EGRESS" // jumped to from DOCKER-USER (forwarded traffic)
	chainInput  = "MECHON-INPUT"  // jumped to from INPUT (traffic addressed to the host)
)

// blockedNets are never reachable from a bot.
var blockedNets = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16", // link-local, cloud metadata
	"100.64.0.0/10",  // CGNAT, also Tailscale
	"0.0.0.0/8",
	"127.0.0.0/8",
	"224.0.0.0/4",   // multicast
	"240.0.0.0/4",   // reserved
	"198.18.0.0/15", // benchmarking, used by some VPNs
}

func iptables(args ...string) error {
	out, err := exec.Command("iptables", append([]string{"-w", "5"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// replaceChain atomically swaps in a new version of chain name holding rules, jumped to from
// parent for packets entering on the bot bridge. The new chain is built under a temporary name
// and the jump is moved before the old chain is dropped, so traffic is never unfiltered.
func replaceChain(parent, name string, rules [][]string) error {
	tmp := name + "-NEW"
	if iptables("-n", "-L", tmp) == nil {
		_ = deleteJumps(parent, tmp)
		_ = iptables("-F", tmp)
	} else if err := iptables("-N", tmp); err != nil {
		return err
	}
	for _, r := range rules {
		if err := iptables(append([]string{"-A", tmp}, r...)...); err != nil {
			return err
		}
	}
	if err := iptables("-I", parent, "1", "-i", bridgeName, "-j", tmp); err != nil {
		return err
	}
	if iptables("-n", "-L", name) == nil {
		if err := deleteJumps(parent, name); err != nil {
			return err
		}
		if err := iptables("-F", name); err != nil {
			return err
		}
		if err := iptables("-X", name); err != nil {
			return err
		}
	}
	return iptables("-E", tmp, name)
}

func deleteJumps(parent, chain string) error {
	for iptables("-C", parent, "-i", bridgeName, "-j", chain) == nil {
		if err := iptables("-D", parent, "-i", bridgeName, "-j", chain); err != nil {
			return err
		}
	}
	return nil
}

// installFirewall (re)writes the Mechon chains and the jumps from DOCKER-USER and INPUT to them.
// Safe to run repeatedly. dnsAllow are resolver IPs a bot may reach on port 53 even though they
// sit in a blocked range (only needed when the operator points bots at a private resolver).
func installFirewall(dns []string) error {
	// Only resolvers inside a blocked range need a hole; public ones are reachable anyway.
	var dnsAllow []string
	for _, s := range dns {
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			continue
		}
		for _, n := range blockedNets {
			if _, cidr, _ := net.ParseCIDR(n); cidr.Contains(ip) {
				dnsAllow = append(dnsAllow, s)
				break
			}
		}
	}
	// DOCKER-USER exists once Docker has started with its iptables integration; create it if
	// Docker has not yet, Docker keeps an existing chain.
	if iptables("-n", "-L", "DOCKER-USER") != nil {
		if err := iptables("-N", "DOCKER-USER"); err != nil {
			return err
		}
	}
	egress := [][]string{
		{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"},
	}
	for _, ip := range dnsAllow {
		egress = append(egress,
			[]string{"-d", ip, "-p", "udp", "--dport", "53", "-j", "RETURN"},
			[]string{"-d", ip, "-p", "tcp", "--dport", "53", "-j", "RETURN"})
	}
	for _, n := range blockedNets {
		egress = append(egress, []string{"-d", n, "-j", "DROP"})
	}
	egress = append(egress, []string{"-j", "RETURN"})
	if err := replaceChain("DOCKER-USER", chainEgress, egress); err != nil {
		return err
	}

	// Anything from a bot addressed to the host itself (any of its addresses) is dropped, apart
	// from replies to connections the host opened.
	input := [][]string{
		{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "RETURN"},
	}
	for _, ip := range dnsAllow {
		input = append(input,
			[]string{"-d", ip, "-p", "udp", "--dport", "53", "-j", "RETURN"},
			[]string{"-d", ip, "-p", "tcp", "--dport", "53", "-j", "RETURN"})
	}
	input = append(input, []string{"-j", "DROP"})
	return replaceChain("INPUT", chainInput, input)
}

// checkFirewall reports whether the jumps into the Mechon chains are still in place (Docker or
// an operator's firewall tool may have flushed them).
func checkFirewall() bool {
	return iptables("-C", "DOCKER-USER", "-i", bridgeName, "-j", chainEgress) == nil &&
		iptables("-C", "INPUT", "-i", bridgeName, "-j", chainInput) == nil &&
		iptables("-C", chainInput, "-j", "DROP") == nil
}
