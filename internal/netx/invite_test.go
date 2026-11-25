package netx

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func inviteFields(t *testing.T) (Invite, string) {
	t.Helper()
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	secret := randomSecret(t)
	return Invite{
		TableID:   "t_7f3a",
		TableName: "周五牌局",
		Host:      "100.76.46.108",
		Port:      3478,
		TLS:       false,
		HostPub:   identity.Pub,
		Secret:    secret,
		HostName:  "ironwater",
		ExpiresAt: 1765000000000,
	}, identity.Pub
}

func TestInviteRoundTripAndForms(t *testing.T) {
	fields, _ := inviteFields(t)
	fields.V = 1
	code, err := CreateInvite(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "POKER1.") {
		t.Fatalf("missing prefix: %.20s", code)
	}
	if len(code) > 4096 {
		t.Fatal("code too long")
	}
	parsed, err := ParseInvite(code)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != fields {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", parsed, fields)
	}

	forms := []string{
		code,
		"  " + code + "  ",
		"poker://join?c=" + code,
		"poker://join?code=" + code,
		"http://100.76.46.108:3478/#" + code,
		"https://ironwater.example-tailnet.ts.net/#" + code,
		"https://ironwater.example-tailnet.ts.net/?invite=" + code,
	}
	url, err := InviteURL(code, "http://100.76.46.108:3478")
	if err != nil {
		t.Fatal(err)
	}
	forms = append(forms, url)
	for _, form := range forms {
		got, err := ParseInvite(form)
		if err != nil {
			t.Fatalf("form %.40s: %v", form, err)
		}
		if got != parsed {
			t.Fatalf("form %.40s parsed differently", form)
		}
	}

	noExpiry := fields
	noExpiry.ExpiresAt = 0
	codeNoExpiry, err := CreateInvite(noExpiry)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseInvite(codeNoExpiry)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != 0 {
		t.Fatalf("expiresAt = %d, want 0", got.ExpiresAt)
	}

	if got, err := InviteURL(code, "http://100.76.46.108:3478/"); err != nil || got != "http://100.76.46.108:3478/#"+code {
		t.Fatalf("InviteURL = %q, %v", got, err)
	}
	if got, err := InviteURL(code, "http://100.76.46.108:3478/?x=1"); err != nil || got != "http://100.76.46.108:3478/#"+code {
		t.Fatalf("InviteURL with query = %q, %v", got, err)
	}
	if _, err := InviteURL("", "http://x"); err == nil {
		t.Fatal("empty code must fail")
	}
	if _, err := InviteURL(code, ""); err == nil {
		t.Fatal("empty base must fail")
	}
}

func TestParseInviteStringExpiry(t *testing.T) {
	fields, _ := inviteFields(t)
	fields.ExpiresAt = 0
	wire := map[string]any{
		"v":         1,
		"tableId":   fields.TableID,
		"tableName": fields.TableName,
		"host":      fields.Host,
		"port":      fields.Port,
		"tls":       false,
		"hostPub":   fields.HostPub,
		"secret":    fields.Secret,
		"hostName":  fields.HostName,
		"expiresAt": "2026-01-02T03:04:05Z",
	}
	raw, _ := json.Marshal(wire)
	got, err := ParseInvite("POKER1." + base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != 1767323045000 {
		t.Fatalf("expiresAt = %d", got.ExpiresAt)
	}

	// null is accepted and means no expiry.
	wire["expiresAt"] = nil
	raw, _ = json.Marshal(wire)
	got, err = ParseInvite("POKER1." + base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != 0 {
		t.Fatalf("null expiresAt = %d", got.ExpiresAt)
	}
}

func TestParseInviteRejectsMalformed(t *testing.T) {
	fields, _ := inviteFields(t)
	base := map[string]any{
		"v":         1,
		"tableId":   fields.TableID,
		"tableName": fields.TableName,
		"host":      fields.Host,
		"port":      fields.Port,
		"tls":       false,
		"hostPub":   fields.HostPub,
		"secret":    fields.Secret,
		"hostName":  fields.HostName,
	}
	craft := func(overrides map[string]any) string {
		merged := map[string]any{}
		for key, value := range base {
			merged[key] = value
		}
		for key, value := range overrides {
			if value == nil {
				delete(merged, key)
			} else {
				merged[key] = value
			}
		}
		raw, _ := json.Marshal(merged)
		return "POKER1." + base64.RawURLEncoding.EncodeToString(raw)
	}
	shortKey := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	malformed := []string{
		"",
		"   ",
		"hello world",
		"POKER1.",
		"POKER2.abc",
		"POKER1." + strings.Repeat("A", 5000),
		craft(map[string]any{"v": 2}),
		craft(map[string]any{"v": "1"}),
		craft(map[string]any{"tableId": 42}),
		craft(map[string]any{"tableId": ""}),
		craft(map[string]any{"tableId": nil}),
		craft(map[string]any{"host": 7}),
		craft(map[string]any{"hostName": nil}),
		craft(map[string]any{"tableName": 3}),
		craft(map[string]any{"port": 0}),
		craft(map[string]any{"port": 70000}),
		craft(map[string]any{"port": "3478"}),
		craft(map[string]any{"port": 3478.5}),
		craft(map[string]any{"tls": "no"}),
		craft(map[string]any{"secret": shortKey}),
		craft(map[string]any{"secret": "not a key"}),
		craft(map[string]any{"secret": nil}),
		craft(map[string]any{"hostPub": shortKey}),
		craft(map[string]any{"hostPub": ""}),
		craft(map[string]any{"expiresAt": "not a date"}),
		craft(map[string]any{"expiresAt": -5}),
		"http://127.0.0.1:3478/",
		"POKER1.not*base64",
	}
	for _, input := range malformed {
		_, err := ParseInvite(input)
		if err == nil {
			t.Fatalf("accepted malformed input %.40q", input)
		}
		if strings.Contains(err.Error(), fields.Secret) {
			t.Fatal("error must not echo the secret")
		}
	}
	// A JSON array is not an object.
	raw, _ := json.Marshal([]any{1})
	if _, err := ParseInvite("POKER1." + base64.RawURLEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("array accepted")
	}
}

func TestCreateInviteValidates(t *testing.T) {
	fields, _ := inviteFields(t)
	bad := fields
	bad.HostPub = "not a key"
	if _, err := CreateInvite(bad); err == nil {
		t.Fatal("bad host key accepted")
	}
	bad = fields
	bad.Port = 0
	if _, err := CreateInvite(bad); err == nil {
		t.Fatal("bad port accepted")
	}
	bad = fields
	bad.TableID = ""
	if _, err := CreateInvite(bad); err == nil {
		t.Fatal("empty table id accepted")
	}
	if _, err := CreateInvite(Invite{}); err == nil {
		t.Fatal("empty invite accepted")
	}
}

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"t_7f3a":       "t_7f3a",
		"p_abcdefghij": "p_abcd",
		"":             "?",
		"  ":           "?",
		"p_ab12":       "p_ab12",
	}
	for input, want := range cases {
		if got := ShortID(input); got != want {
			t.Fatalf("ShortID(%q) = %q, want %q", input, got, want)
		}
	}
}
