package wireguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture mimics a wg0.conf created by the reference wireguard-install script,
// including an IPv6 setup and two clients.
const fixture = `# Do not alter the commented lines
# They are used by wireguard-install
# ENDPOINT 203.0.113.10

[Interface]
Address = 10.7.0.1/24, fddd:2c4:2c4:2c4::1/64
PrivateKey = SERVERPRIVKEY=
ListenPort = 51820

# BEGIN_PEER alice
[Peer]
PublicKey = ALICEPUB=
PresharedKey = ALICEPSK=
AllowedIPs = 10.7.0.2/32, fddd:2c4:2c4:2c4::2/128
# END_PEER alice
# BEGIN_PEER bob
[Peer]
PublicKey = BOBPUB=
PresharedKey = BOBPSK=
AllowedIPs = 10.7.0.3/32
# END_PEER bob
`

func TestParse(t *testing.T) {
	cfg, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "203.0.113.10" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.ListenPort != "51820" {
		t.Errorf("port = %q", cfg.ListenPort)
	}
	if !cfg.HasIPv6 {
		t.Error("HasIPv6 = false, want true")
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("peers = %d, want 2", len(cfg.Peers))
	}
	if cfg.Peers[0].Name != "alice" || cfg.Peers[0].AllowedIPs != "10.7.0.2/32, fddd:2c4:2c4:2c4::2/128" {
		t.Errorf("alice parsed wrong: %+v", cfg.Peers[0])
	}
	if cfg.Peers[1].Name != "bob" || cfg.Peers[1].AllowedIPs != "10.7.0.3/32" {
		t.Errorf("bob parsed wrong: %+v", cfg.Peers[1])
	}
}

// Round-trip must be byte-identical for a Nyr-produced config.
func TestSerializeRoundTrip(t *testing.T) {
	cfg, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Serialize(); got != fixture {
		t.Errorf("round-trip mismatch:\n%q\n%q", fixture, got)
	}
}

func TestBlockedRoundTrip(t *testing.T) {
	cfg, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Peers[0].Blocked = true

	// Blocked peers survive a round-trip and remain marked.
	cfg2, err := Parse(cfg.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Peers) != 2 || !cfg2.Peers[0].Blocked {
		t.Fatalf("blocked flag lost: %+v", cfg2.Peers)
	}
	if cfg2.Peers[0].Name != "alice" || cfg2.Peers[0].PublicKey != "ALICEPUB=" {
		t.Errorf("blocked peer fields lost: %+v", cfg2.Peers[0])
	}
	if cfg2.Peers[1].Blocked {
		t.Error("bob must not be blocked")
	}

	// The blocked block must be ignored by wg-quick: every data line commented.
	for _, line := range strings.Split(cfg.Serialize(), "\n") {
		if strings.Contains(line, "ALICEPUB") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("blocked peer line not commented: %q", line)
		}
	}
}

func TestFreeOctet(t *testing.T) {
	cfg, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if o, _ := cfg.FreeOctet(); o != 4 {
		t.Errorf("free octet = %d, want 4", o)
	}
	// Blocked peers still occupy their address.
	cfg.Peers[1].Blocked = true
	if o, _ := cfg.FreeOctet(); o != 4 {
		t.Errorf("free octet with blocked peer = %d, want 4", o)
	}
}

func TestSave(t *testing.T) {
	cfg, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wg0.conf")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != fixture {
		t.Error("saved file differs from fixture")
	}
}
