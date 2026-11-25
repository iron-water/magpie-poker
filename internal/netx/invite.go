package netx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	invitePrefix = "POKER1."
	inviteMaxLen = 4096
)

var schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// Invite is the table invite from docs/PROTOCOL.md. ExpiresAt is unix
// milliseconds; 0 means no expiry.
type Invite struct {
	V         int    `json:"v"`
	TableID   string `json:"tableId"`
	TableName string `json:"tableName"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TLS       bool   `json:"tls"`
	HostPub   string `json:"hostPub"`
	Secret    string `json:"secret"`
	HostName  string `json:"hostName"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

type inviteWire struct {
	V         int    `json:"v"`
	TableID   string `json:"tableId"`
	TableName string `json:"tableName"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TLS       bool   `json:"tls"`
	HostPub   string `json:"hostPub"`
	Secret    string `json:"secret"`
	HostName  string `json:"hostName"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
}

// CreateInvite validates fields and returns "POKER1."+base64url(JSON).
func CreateInvite(inv Invite) (string, error) {
	wire := inviteWire{
		V:         1,
		TableID:   inv.TableID,
		TableName: inv.TableName,
		Host:      inv.Host,
		Port:      inv.Port,
		TLS:       inv.TLS,
		HostPub:   inv.HostPub,
		Secret:    inv.Secret,
		HostName:  inv.HostName,
	}
	if inv.ExpiresAt > 0 {
		expires := inv.ExpiresAt
		wire.ExpiresAt = &expires
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	code := invitePrefix + base64.RawURLEncoding.EncodeToString(payload)
	if len(code) > inviteMaxLen {
		return "", errors.New("邀请码过长")
	}
	// Whatever we emit must parse back.
	if _, err := ParseInvite(code); err != nil {
		return "", err
	}
	return code, nil
}

// ParseInvite accepts a raw code, a poker:// URL, an http(s) URL with the
// code in the fragment, or a URL with ?invite=/?c=/?code=.
func ParseInvite(input string) (Invite, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Invite{}, errors.New("邀请码为空")
	}
	if len(raw) > inviteMaxLen {
		return Invite{}, errors.New("邀请码过长")
	}
	code, ok := findCode(raw, 0)
	if !ok {
		return Invite{}, errors.New("不是有效的牌桌邀请码")
	}
	payload := strings.TrimPrefix(code, invitePrefix)
	if payload == "" {
		return Invite{}, errors.New("邀请码缺少内容")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
	}
	if err != nil {
		return Invite{}, errors.New("邀请码不是合法的 base64url")
	}
	var doc any
	if err := json.Unmarshal(decoded, &doc); err != nil {
		return Invite{}, errors.New("邀请码内容无法解析")
	}
	return normalizeInvite(doc)
}

func findCode(value string, depth int) (string, bool) {
	if depth > 3 {
		return "", false
	}
	text := strings.TrimSpace(value)
	if text == "" {
		return "", false
	}
	if strings.HasPrefix(text, invitePrefix) {
		return text, true
	}
	if !schemeRE.MatchString(text) {
		return "", false
	}
	u, err := url.Parse(text)
	if err != nil {
		return "", false
	}
	for _, key := range []string{"c", "invite", "code"} {
		if vals, ok := u.Query()[key]; ok && len(vals) > 0 && vals[0] != "" {
			if found, ok := findCode(vals[0], depth+1); ok {
				return found, true
			}
			break
		}
	}
	if u.Fragment != "" {
		fragment, err := url.PathUnescape(u.Fragment)
		if err != nil {
			return "", false
		}
		if found, ok := findCode(fragment, depth+1); ok {
			return found, true
		}
	}
	return "", false
}

func requireText(obj map[string]any, key, label string, max int) (string, error) {
	value, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("%s 必须是字符串", label)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s 必须是字符串", label)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s 不能为空", label)
	}
	if utf8.RuneCountInString(text) > max {
		return "", fmt.Errorf("%s 过长", label)
	}
	return text, nil
}

func truncateRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}

func parseExpiresAt(value any) (int64, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return 0, errors.New("过期时间无效")
		}
		return int64(v), nil
	case string:
		text := strings.TrimSpace(v)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed.UnixMilli(), nil
			}
		}
		return 0, errors.New("过期时间无效")
	default:
		return 0, errors.New("过期时间无效")
	}
}

func normalizeInvite(doc any) (Invite, error) {
	obj, ok := doc.(map[string]any)
	if !ok {
		return Invite{}, errors.New("邀请内容不是 JSON 对象")
	}
	version, ok := obj["v"].(float64)
	if !ok || version != 1 {
		return Invite{}, errors.New("邀请码版本不支持")
	}
	tableID, err := requireText(obj, "tableId", "牌桌 id", 64)
	if err != nil {
		return Invite{}, err
	}
	tableName, ok := obj["tableName"].(string)
	if !ok {
		return Invite{}, errors.New("桌名必须是字符串")
	}
	tableName = truncateRunes(strings.TrimSpace(tableName), 80)
	host, err := requireText(obj, "host", "主机地址", 255)
	if err != nil {
		return Invite{}, err
	}
	hostName, err := requireText(obj, "hostName", "主机名", 64)
	if err != nil {
		return Invite{}, err
	}
	portValue, ok := obj["port"].(float64)
	if !ok || portValue != math.Trunc(portValue) || portValue < 1 || portValue > 65535 {
		return Invite{}, errors.New("端口无效")
	}
	tls := false
	if rawTLS, exists := obj["tls"]; exists {
		flag, ok := rawTLS.(bool)
		if !ok {
			return Invite{}, errors.New("tls 必须是布尔值")
		}
		tls = flag
	}
	hostPub, ok := obj["hostPub"].(string)
	if !ok {
		return Invite{}, errors.New("主机公钥必须是字符串")
	}
	if _, err := decodeKey(hostPub, "主机公钥"); err != nil {
		return Invite{}, err
	}
	secret, ok := obj["secret"].(string)
	if !ok {
		return Invite{}, errors.New("牌桌密钥必须是字符串")
	}
	if _, err := decodeKey(secret, "牌桌密钥"); err != nil {
		return Invite{}, err
	}
	expiresAt, err := parseExpiresAt(obj["expiresAt"])
	if err != nil {
		return Invite{}, err
	}
	return Invite{
		V:         1,
		TableID:   tableID,
		TableName: tableName,
		Host:      host,
		Port:      int(portValue),
		TLS:       tls,
		HostPub:   hostPub,
		Secret:    secret,
		HostName:  hostName,
		ExpiresAt: expiresAt,
	}, nil
}

// InviteURL builds a page link with the code in the fragment.
func InviteURL(code, baseURL string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if idx := strings.IndexAny(base, "?#"); idx >= 0 {
		base = base[:idx]
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return "", errors.New("缺少主机地址")
	}
	value := strings.TrimSpace(code)
	if value == "" {
		return "", errors.New("邀请码为空")
	}
	return base + "/#" + value, nil
}

// ShortID trims an id for display.
func ShortID(id string) string {
	text := strings.TrimSpace(id)
	if text == "" {
		return "?"
	}
	runes := []rune(text)
	if len(runes) <= 6 {
		return text
	}
	return string(runes[:6])
}
