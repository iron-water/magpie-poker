package magpiecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Key struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type KeysState struct {
	Magpie bool   `json:"magpie"`
	Bin    string `json:"bin"`
	Issuer *Key   `json:"issuer"`
	All    []Key  `json:"all"`
	Error  string `json:"error,omitempty"`
}

func ConfigDir() string {
	if d := os.Getenv("MAGPIE_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "magpie")
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".config/magpie"
	}
	return filepath.Join(h, ".config", "magpie")
}

var cachedBin string
var binOnce bool

func Bin() string {
	if binOnce {
		return cachedBin
	}
	binOnce = true
	cands := []string{os.Getenv("MAGPIE_BIN")}
	local := os.Getenv("LOCALAPPDATA")
	if local != "" {
		cands = append(cands,
			filepath.Join(local, "Programs", "magpie", "magpie.com"),
			filepath.Join(local, "magpie", "magpie.com"),
		)
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		cands = append(cands, filepath.Join(pf, "magpie", "magpie.com"))
	}
	cands = append(cands, "magpie.com", "magpie")
	for _, c := range cands {
		if c == "" {
			continue
		}
		if strings.ContainsAny(c, `/\`) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				cachedBin = c
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			cachedBin = p
			return p
		}
	}
	return ""
}

func run(timeout time.Duration, args ...string) (string, error) {
	bin := Bin()
	if bin == "" {
		return "", errors.New("没有找到 magpie 命令行（可用 MAGPIE_BIN 指定）")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	hide(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(Redact(msg))
	}
	return out.String(), nil
}

var keyRow = regexp.MustCompile(`^([0-9a-f]{24})\s{2,}(.+)$`)

func ListKeys() ([]Key, error) {
	out, err := run(25*time.Second, "gateway-key", "list")
	if err != nil {
		return nil, err
	}
	var keys []Key
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		m := keyRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		parts := regexp.MustCompile(`\s{2,}`).Split(m[2], -1)
		name := ""
		state := ""
		if len(parts) > 0 {
			name = parts[0]
		}
		if len(parts) > 1 {
			state = parts[1]
		}
		keys = append(keys, Key{ID: m[1], Name: name, State: state})
	}
	return keys, nil
}

func EnsureBank(preferred string) (*Key, error) {
	before, err := ListKeys()
	if err != nil {
		return nil, err
	}
	var key *Key
	for i := range before {
		if preferred != "" && before[i].ID == preferred {
			key = &before[i]
			break
		}
		if preferred == "" && before[i].Name == "poker-bank" {
			key = &before[i]
			break
		}
	}
	if key == nil {
		if _, err := run(30*time.Second, "gateway-key", "add", "poker-bank"); err != nil {
			return nil, err
		}
		after, err := ListKeys()
		if err != nil {
			return nil, err
		}
		old := map[string]bool{}
		for _, k := range before {
			old[k.ID] = true
		}
		for i := range after {
			if after[i].Name == "poker-bank" && !old[after[i].ID] {
				key = &after[i]
				break
			}
		}
		if key == nil {
			for i := range after {
				if after[i].Name == "poker-bank" {
					key = &after[i]
					break
				}
			}
		}
	}
	if key == nil {
		return nil, errors.New("创建 poker-bank 失败")
	}
	if _, err := run(20*time.Second, "gateway-key", "issuer", key.ID, "on"); err != nil {
		return nil, err
	}
	return key, nil
}

func IssueKey(issuerID, name, unit string, amount float64) (string, string, error) {
	if issuerID == "" {
		return "", "", errors.New("没有可用的 issuer key")
	}
	if amount <= 0 {
		return "", "", errors.New("结算额度不合法")
	}
	safe := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(name, "-")
	if len(safe) > 48 {
		safe = safe[:48]
	}
	args := []string{"gateway-key", "issue", "--from", issuerID, "--ttl", "168h", "--name", safe, "--json"}
	if unit == "tokens" {
		n := int(amount)
		if n < 1 {
			n = 1
		}
		args = append(args, "--tokens", strconv.Itoa(n))
	} else {
		args = append(args, "--cost", fmt.Sprintf("%.4f", amount))
	}
	out, err := run(35*time.Second, args...)
	if err != nil {
		return "", "", err
	}
	s := strings.Index(out, "{")
	e := strings.LastIndex(out, "}")
	if s < 0 || e <= s {
		return "", "", errors.New("magpie 没有返回 key")
	}
	var data struct {
		Key     string `json:"key"`
		Expires string `json:"expires"`
	}
	if err := json.Unmarshal([]byte(out[s:e+1]), &data); err != nil {
		return "", "", errors.New("magpie 返回的 key 不是 JSON")
	}
	if data.Key == "" {
		return "", "", errors.New("magpie 没有返回 key")
	}
	return data.Key, data.Expires, nil
}

func GatewayURL() string {
	if addr := os.Getenv("MAGPIE_ADDR"); addr != "" && regexp.MustCompile(`^[\w.-]+:\d+$`).MatchString(addr) {
		return "http://" + addr
	}
	dir := ConfigDir()
	for _, f := range []string{filepath.Join(dir, "gateway.json"), filepath.Join(dir, "settings.json")} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var j map[string]any
		if json.Unmarshal(b, &j) != nil {
			continue
		}
		if u, ok := j["url"].(string); ok && (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
			return strings.TrimRight(u, "/")
		}
		for _, k := range []string{"port", "gatewayPort", "listen"} {
			if n, ok := j[k].(float64); ok && n > 0 && n < 65536 {
				return fmt.Sprintf("http://127.0.0.1:%d", int(n))
			}
		}
	}
	return "http://127.0.0.1:3425"
}

func VerifyKey(rawURL, key string) (bool, int, int, string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false, 0, 0, "网关地址不合法"
	}
	u.Path = strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(u.Path, "/v1/models"):
	case strings.HasSuffix(u.Path, "/v1"):
		u.Path += "/models"
	default:
		u.Path += "/v1/models"
	}
	u.RawQuery = ""
	u.Fragment = ""
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false, 0, 0, err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, 0, Redact(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, 0, resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	models := 0
	var j struct {
		Data   []any `json:"data"`
		Models []any `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&j) == nil {
		if len(j.Data) > 0 {
			models = len(j.Data)
		} else {
			models = len(j.Models)
		}
	}
	return true, models, resp.StatusCode, ""
}

func Redact(s string) string {
	return regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`).ReplaceAllString(s, "sk-***")
}
