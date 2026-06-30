package cfsetup

import (
	"bufio"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// State records what `zorail setup` provisioned, so `zorail doctor` and repeat
// runs can find and verify the same resources.
type State struct {
	Domain    string `json:"domain"`
	Hostname  string `json:"hostname"` // public ingress hostname (mail.<domain>)
	Origin    string `json:"origin"`   // local origin the tunnel points at
	ZoneID    string `json:"zone_id"`
	AccountID string `json:"account_id"`
	TunnelID  string `json:"tunnel_id"`
	Worker    string `json:"worker"` // deployed Worker script name
}

func statePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zorail", "zmail-setup.json"), nil
}

func SaveState(s *State) error {
	p, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func LoadState() (*State, error) {
	p, err := statePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// newToken returns a strong, URL-safe token suitable for ZORAIL_API_TOKEN.
func newToken() string {
	var b [20]byte
	_, _ = rand.Read(b[:])
	return "zt_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// readEnvValue returns the value of key in a dotenv-style file, or "".
func readEnvValue(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// upsertEnvValue sets key=value in a dotenv file, preserving other lines and
// creating the file if needed.
func upsertEnvValue(path, key, value string) error {
	var lines []string
	found := false
	if b, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
			if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
				lines = append(lines, key+"="+value)
				found = true
				continue
			}
			lines = append(lines, line)
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}
