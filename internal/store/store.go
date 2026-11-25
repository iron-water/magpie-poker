package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Profile struct {
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	GatewayURL string `json:"gatewayUrl"`
}

type HostRecord struct {
	TableID      string `json:"tableId"`
	HostPlayerID string `json:"hostPlayerId"`
	Pub          string `json:"pub"`
	Priv         string `json:"priv"`
	Secret       string `json:"secret"`
	Port         int    `json:"port"`
	ServedURL    string `json:"servedUrl,omitempty"`
}

type Bank struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Store struct {
	Dir string
}

func DataDir() string {
	if d := os.Getenv("MAGPIE_POKER_DATA"); d != "" {
		return d
	}
	base := os.Getenv("MAGPIE_CONFIG_DIR")
	if base == "" {
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			base = filepath.Join(x, "magpie")
		} else if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".config", "magpie")
		}
	}
	if base == "" {
		base = ".config/magpie"
	}
	return filepath.Join(base, "magpie-poker")
}

func New(dir string) (*Store, error) {
	if dir == "" {
		dir = DataDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) == 0 || strings.TrimSpace(string(b)) == "null" {
		return errors.New("empty")
	}
	return json.Unmarshal(b, v)
}

func (s *Store) write(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(name) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(name)); err != nil {
		os.Remove(tmp)
		return os.WriteFile(s.path(name), b, 0o600)
	}
	return nil
}

func defaultName() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "玩家"
	}
	parts := strings.FieldsFunc(h, func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) == 0 {
		return "玩家"
	}
	return parts[len(parts)-1]
}

func (s *Store) LoadProfile() Profile {
	var p Profile
	if readJSON(s.path("profile.json"), &p) != nil || p.Name == "" {
		p = Profile{Name: defaultName(), Avatar: "fox"}
	}
	if p.Avatar == "" {
		p.Avatar = "fox"
	}
	return p
}

func (s *Store) SaveProfile(p Profile) error { return s.write("profile.json", p) }

func (s *Store) LoadHost() (*HostRecord, error) {
	var h HostRecord
	if err := readJSON(s.path("host.json"), &h); err != nil {
		return nil, err
	}
	if h.TableID == "" || h.HostPlayerID == "" || h.Secret == "" || h.Pub == "" || h.Priv == "" {
		return nil, errors.New("host record incomplete")
	}
	return &h, nil
}

func (s *Store) SaveHost(h *HostRecord) error {
	if h == nil {
		return os.Remove(s.path("host.json"))
	}
	return s.write("host.json", h)
}

func (s *Store) ClearHost() {
	os.Remove(s.path("host.json"))
}

func (s *Store) LoadTable(id string) ([]byte, error) {
	if id == "" {
		return nil, errors.New("empty table id")
	}
	return os.ReadFile(s.path("table-" + id + ".json"))
}

func (s *Store) SaveTable(id string, data []byte) error {
	if id == "" {
		return nil
	}
	tmp := s.path("table-" + id + ".json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path("table-"+id+".json")); err != nil {
		os.Remove(tmp)
		return os.WriteFile(s.path("table-"+id+".json"), data, 0o600)
	}
	return nil
}

func (s *Store) LoadBank() *Bank {
	var b Bank
	if readJSON(s.path("bank.json"), &b) != nil || b.ID == "" {
		return nil
	}
	return &b
}

func (s *Store) SaveBank(b *Bank) error {
	if b == nil {
		return os.Remove(s.path("bank.json"))
	}
	return s.write("bank.json", b)
}
