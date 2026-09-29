// Package wireguard parses and writes /etc/wireguard/wg0.conf in the exact
// format produced by the Nyr wireguard-install script, so both tools can
// manage the same file interchangeably.
package wireguard

import (
	"fmt"
	"os"
	"strings"
)

// Peer is a client block delimited by "# BEGIN_PEER <name>" / "# END_PEER <name>".
type Peer struct {
	Name         string
	PublicKey    string
	PresharedKey string
	AllowedIPs   string // e.g. "10.7.0.3/32" or "10.7.0.3/32, fddd:2c4:2c4:2c4::3/128"
	Blocked      bool
}

// Config is a parsed wg0.conf. The raw header (everything before the first
// peer block) is preserved verbatim so round-trips never break the file.
type Config struct {
	Endpoint   string // from "# ENDPOINT <ip>"
	Address    string // e.g. "10.7.0.1/24" (+ ", fddd:2c4:2c4:2c4::1/64")
	PrivateKey string
	ListenPort string
	HasIPv6    bool
	Peers      []Peer

	header []string // raw lines before the first peer block, verbatim
}

const (
	ipv6Prefix = "fddd:2c4:2c4:2c4::1"
	beginMark  = "# BEGIN_PEER "
	endMark    = "# END_PEER "
)

// Load reads and parses a wg0.conf file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data))
}

// Parse parses wg0.conf content.
func Parse(content string) (*Config, error) {
	cfg := &Config{}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	var cur *Peer
	var block []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, beginMark) {
			if cur != nil {
				return nil, fmt.Errorf("nested BEGIN_PEER block")
			}
			cur = &Peer{Name: strings.TrimSpace(strings.TrimPrefix(trimmed, beginMark))}
			block = nil
			continue
		}
		if strings.HasPrefix(trimmed, endMark) {
			if cur == nil {
				return nil, fmt.Errorf("END_PEER without BEGIN_PEER")
			}
			parseBlock(cur, block)
			cfg.Peers = append(cfg.Peers, *cur)
			cur = nil
			continue
		}
		if cur != nil {
			block = append(block, line)
			continue
		}
		cfg.header = append(cfg.header, line)
	}
	if cur != nil {
		return nil, fmt.Errorf("unterminated BEGIN_PEER block for %q", cur.Name)
	}

	// Header fields.
	for _, line := range cfg.header {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# ENDPOINT "):
			cfg.Endpoint = strings.TrimSpace(strings.TrimPrefix(trimmed, "# ENDPOINT "))
		case strings.HasPrefix(trimmed, "Address = "):
			cfg.Address = strings.TrimSpace(strings.TrimPrefix(trimmed, "Address = "))
		case strings.HasPrefix(trimmed, "PrivateKey = "):
			cfg.PrivateKey = strings.TrimSpace(strings.TrimPrefix(trimmed, "PrivateKey = "))
		case strings.HasPrefix(trimmed, "ListenPort = "):
			cfg.ListenPort = strings.TrimSpace(strings.TrimPrefix(trimmed, "ListenPort = "))
		}
	}
	cfg.HasIPv6 = strings.Contains(cfg.Address, ipv6Prefix)
	return cfg, nil
}

// parseBlock parses the body of a peer block. Blocked peers (written by wgbot)
// have every line commented with a leading '#'.
func parseBlock(p *Peer, block []string) {
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "# BLOCKED") {
			if strings.Contains(trimmed, "BLOCKED") {
				p.Blocked = true
			}
			continue
		}
		wasCommented := strings.HasPrefix(trimmed, "#")
		body := strings.TrimPrefix(trimmed, "#")
		body = strings.TrimSpace(body)
		switch {
		case body == "[Peer]":
			if wasCommented {
				p.Blocked = true
			}
		case strings.HasPrefix(body, "PublicKey = "):
			p.PublicKey = strings.TrimSpace(strings.TrimPrefix(body, "PublicKey = "))
		case strings.HasPrefix(body, "PresharedKey = "):
			p.PresharedKey = strings.TrimSpace(strings.TrimPrefix(body, "PresharedKey = "))
		case strings.HasPrefix(body, "AllowedIPs = "):
			p.AllowedIPs = strings.TrimSpace(strings.TrimPrefix(body, "AllowedIPs = "))
		}
	}
}

// Serialize renders the config back to wg0.conf format (Nyr-compatible).
// Blocked peers are written fully commented out inside their markers, which
// keeps the block visible to both tools while wg-quick ignores it.
func (c *Config) Serialize() string {
	var b strings.Builder
	for _, line := range c.header {
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, p := range c.Peers {
		b.WriteString(beginMark + p.Name + "\n")
		comment := ""
		if p.Blocked {
			comment = "#"
		}
		fmt.Fprintf(&b, "%s[Peer]\n", comment)
		fmt.Fprintf(&b, "%sPublicKey = %s\n", comment, p.PublicKey)
		fmt.Fprintf(&b, "%sPresharedKey = %s\n", comment, p.PresharedKey)
		fmt.Fprintf(&b, "%sAllowedIPs = %s\n", comment, p.AllowedIPs)
		b.WriteString(endMark + p.Name + "\n")
	}
	return b.String()
}

// Save writes the config to path with 0600 permissions (atomic via temp file).
func (c *Config) Save(path string) error {
	tmp := path + ".wgbot.tmp"
	if err := os.WriteFile(tmp, []byte(c.Serialize()), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// PeerByName returns the peer with the given name.
func (c *Config) PeerByName(name string) (*Peer, bool) {
	for i := range c.Peers {
		if c.Peers[i].Name == name {
			return &c.Peers[i], true
		}
	}
	return nil, false
}

// SerializePeerSnippet renders one peer's block as accepted by `wg addconf`.
func (c *Config) SerializePeerSnippet(name string) string {
	for _, p := range c.Peers {
		if p.Name != name {
			continue
		}
		return fmt.Sprintf("[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s\n",
			p.PublicKey, p.PresharedKey, p.AllowedIPs)
	}
	return ""
}

// FreeOctet returns the lowest still-available host octet, starting at 2
// (1 is the gateway), mirroring the reference script's logic.
func (c *Config) FreeOctet() (int, error) {
	used := map[int]bool{}
	for _, p := range c.Peers {
		for _, ip := range strings.Split(p.AllowedIPs, ",") {
			ip = strings.TrimSpace(ip)
			if !strings.Contains(ip, ".") {
				continue
			}
			host := ip
			if i := strings.Index(host, "/"); i >= 0 {
				host = host[:i]
			}
			parts := strings.Split(host, ".")
			if len(parts) != 4 {
				continue
			}
			var n int
			if _, err := fmt.Sscanf(parts[3], "%d", &n); err == nil {
				used[n] = true
			}
		}
	}
	for octet := 2; octet <= 254; octet++ {
		if !used[octet] {
			return octet, nil
		}
	}
	return 0, fmt.Errorf("the WireGuard internal subnet is full (253 clients)")
}
