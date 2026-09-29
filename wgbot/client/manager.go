// Package client implements add/block/unblock/delete of WireGuard peers with
// exactly the same semantics as the reference wireguard-install script.
package client

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"wgbot/wireguard"
)

var nameRe = regexp.MustCompile(`^[0-9a-zA-Z_-]{1,15}$`)

// Manager owns /etc/wireguard/wg0.conf mutations.
type Manager struct {
	ConfPath   string
	Iface      string
	ClientsDir string
}

// New creates a Manager.
func New(confPath, iface, clientsDir string) *Manager {
	return &Manager{ConfPath: confPath, Iface: iface, ClientsDir: clientsDir}
}

// Client is a peer plus its on-disk config path.
type Client struct {
	Name       string
	PublicKey  string
	AllowedIPs string
	Blocked    bool
	ConfPath   string
}

// List returns all configured clients.
func (m *Manager) List() ([]Client, error) {
	cfg, err := wireguard.Load(m.ConfPath)
	if err != nil {
		return nil, err
	}
	var out []Client
	for _, p := range cfg.Peers {
		out = append(out, Client{
			Name:       p.Name,
			PublicKey:  p.PublicKey,
			AllowedIPs: p.AllowedIPs,
			Blocked:    p.Blocked,
			ConfPath:   filepath.Join(m.ClientsDir, p.Name+".conf"),
		})
	}
	return out, nil
}

// ValidName mirrors the reference script's client name sanitization rules.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("имя: только латиница, цифры, «-», «_», до 15 символов")
	}
	return nil
}

// Add creates a new client exactly like new_client_setup() in the reference
// script: lowest free octet, fresh keys + PSK, peer appended to wg0.conf
// between BEGIN_PEER/END_PEER markers, applied live via `wg addconf` (no restart).
// Returns the client config file path and its content.
func (m *Manager) Add(name, dns string) (path string, content string, err error) {
	if err := ValidName(name); err != nil {
		return "", "", err
	}
	cfg, err := wireguard.Load(m.ConfPath)
	if err != nil {
		return "", "", err
	}
	if _, exists := cfg.PeerByName(name); exists {
		return "", "", fmt.Errorf("клиент %q уже существует", name)
	}
	octet, err := cfg.FreeOctet()
	if err != nil {
		return "", "", err
	}

	key, err := wireguard.GenKey()
	if err != nil {
		return "", "", err
	}
	psk, err := wireguard.GenPSK()
	if err != nil {
		return "", "", err
	}
	pub, err := wireguard.PubKey(key)
	if err != nil {
		return "", "", err
	}

	allowed := fmt.Sprintf("10.7.0.%d/32", octet)
	if cfg.HasIPv6 {
		allowed += fmt.Sprintf(", fddd:2c4:2c4:2c4::%d/128", octet)
	}
	cfg.Peers = append(cfg.Peers, wireguard.Peer{
		Name:         name,
		PublicKey:    pub,
		PresharedKey: psk,
		AllowedIPs:   allowed,
	})

	// Build client config before touching anything.
	serverPub, err := wireguard.PubKey(cfg.PrivateKey)
	if err != nil {
		return "", "", err
	}
	address := fmt.Sprintf("10.7.0.%d/24", octet)
	if cfg.HasIPv6 {
		address += fmt.Sprintf(", fddd:2c4:2c4:2c4::%d/64", octet)
	}
	content = fmt.Sprintf(`[Interface]
Address = %s
DNS = %s
PrivateKey = %s

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = %s:%s
PersistentKeepalive = 25
`, address, dns, key, serverPub, psk, cfg.Endpoint, cfg.ListenPort)

	if err := cfg.Save(m.ConfPath); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(m.ClientsDir, 0700); err != nil {
		return "", "", err
	}
	path = filepath.Join(m.ClientsDir, name+".conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", "", err
	}
	if err := wireguard.AddConf(m.Iface, cfg.SerializePeerSnippet(name)); err != nil {
		return path, content, fmt.Errorf("клиент добавлен в конфиг, но не применён к живому интерфейсу: %w", err)
	}
	return path, content, nil
}

// Block removes the peer from the live interface and comments out its block
// in wg0.conf (keeping the address reserved).
func (m *Manager) Block(name string) error {
	cfg, err := wireguard.Load(m.ConfPath)
	if err != nil {
		return err
	}
	p, ok := cfg.PeerByName(name)
	if !ok {
		return fmt.Errorf("клиент %q не найден", name)
	}
	if !p.Blocked {
		if err := wireguard.RemovePeer(m.Iface, p.PublicKey); err != nil {
			return err
		}
		p.Blocked = true
		if err := cfg.Save(m.ConfPath); err != nil {
			return err
		}
	}
	return nil
}

// Unblock restores a blocked peer: uncomments its block and re-adds it live.
func (m *Manager) Unblock(name string) error {
	cfg, err := wireguard.Load(m.ConfPath)
	if err != nil {
		return err
	}
	p, ok := cfg.PeerByName(name)
	if !ok {
		return fmt.Errorf("клиент %q не найден", name)
	}
	if p.Blocked {
		p.Blocked = false
		if err := cfg.Save(m.ConfPath); err != nil {
			return err
		}
		if err := wireguard.AddConf(m.Iface, cfg.SerializePeerSnippet(name)); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes the peer both from the live interface and from wg0.conf,
// exactly like option 2 of the reference script, and removes the client .conf.
func (m *Manager) Delete(name string) error {
	cfg, err := wireguard.Load(m.ConfPath)
	if err != nil {
		return err
	}
	p, ok := cfg.PeerByName(name)
	if !ok {
		return fmt.Errorf("клиент %q не найден", name)
	}
	if !p.Blocked {
		if err := wireguard.RemovePeer(m.Iface, p.PublicKey); err != nil {
			return err
		}
	}
	var kept []wireguard.Peer
	for _, peer := range cfg.Peers {
		if peer.Name != name {
			kept = append(kept, peer)
		}
	}
	cfg.Peers = kept
	if err := cfg.Save(m.ConfPath); err != nil {
		return err
	}
	os.Remove(filepath.Join(m.ClientsDir, name+".conf"))
	return nil
}

// SystemDNS mimics option 1 of the reference script: use the system
// resolvers (with the systemd-resolved fallback path).
func SystemDNS() string {
	candidates := []string{"/etc/resolv.conf", "/run/systemd/resolve/resolv.conf"}
	var nameservers []string
	for _, f := range candidates {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "nameserver") {
				continue
			}
			ns := strings.TrimSpace(strings.TrimPrefix(line, "nameserver"))
			if ns == "127.0.0.53" || strings.Contains(ns, ":") {
				continue // stub resolver / IPv6
			}
			nameservers = append(nameservers, ns)
		}
		if len(nameservers) > 0 {
			break
		}
	}
	if len(nameservers) == 0 {
		return "1.1.1.1, 8.8.8.8"
	}
	return strings.Join(nameservers, ", ")
}

// DNSChoices returns the same 8 options as new_client_dns() in the reference.
func DNSChoices() []string {
	return []string{
		SystemDNS(),
		"8.8.8.8, 8.8.4.4",
		"1.1.1.1, 1.0.0.1",
		"208.67.222.222, 208.67.220.220",
		"9.9.9.9, 149.112.112.112",
		"95.85.95.85, 2.56.220.2",
		"94.140.14.14, 94.140.15.15",
		"", // custom, filled by the user
	}
}
