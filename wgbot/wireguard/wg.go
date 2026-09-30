package wireguard

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PeerStat is live state of a peer from `wg show <iface> dump`.
type PeerStat struct {
	PublicKey     string
	Endpoint      string
	AllowedIPs    string
	LastHandshake time.Time // zero = never
	Rx, Tx        uint64
}

// GenKey runs `wg genkey`.
func GenKey() (string, error) {
	return runWG("genkey")
}

// GenPSK runs `wg genpsk`.
func GenPSK() (string, error) {
	return runWG("genpsk")
}

// PubKey derives the public key from a private key via `wg pubkey`.
func PubKey(privateKey string) (string, error) {
	return runWGStdinImpl(privateKey, "pubkey")
}

// Dump parses `wg show <iface> dump` output.
func Dump(iface string) ([]PeerStat, error) {
	out, err := runWG("show", iface, "dump")
	if err != nil {
		return nil, err
	}
	var stats []PeerStat
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if i == 0 || line == "" {
			continue // first line is the interface itself
		}
		f := strings.Split(line, "\t")
		if len(f) < 8 {
			continue
		}
		var hs int64
		fmt.Sscanf(f[4], "%d", &hs)
		var rx, tx uint64
		fmt.Sscanf(f[5], "%d", &rx)
		fmt.Sscanf(f[6], "%d", &tx)
		s := PeerStat{
			PublicKey:  f[0],
			Endpoint:   f[2],
			AllowedIPs: f[3],
			Rx:         rx,
			Tx:         tx,
		}
		if hs > 0 {
			s.LastHandshake = time.Unix(hs, 0)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// AddConf applies a peer config snippet to a running interface without
// restarting it (same as the reference script's `wg addconf` step).
// The snippet is piped through /dev/stdin: no temp files, no permission
// issues regardless of how wgbot is launched (the wg binary opens the
// file itself, so /tmp can be inaccessible to it).
func AddConf(iface, snippet string) error {
	cmd := exec.Command("wg", "addconf", iface, "/dev/stdin")
	cmd.Stdin = strings.NewReader(snippet)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg addconf %s: %s", iface, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemovePeer removes a peer from the live interface without touching others.
func RemovePeer(iface, publicKey string) error {
	_, err := runWG("set", iface, "peer", publicKey, "remove")
	return err
}

func runWG(args ...string) (string, error) {
	out, err := exec.Command("wg", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("wg %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runWGStdinImpl(stdin string, args ...string) (string, error) {
	cmd := exec.Command("wg", args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("wg %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
