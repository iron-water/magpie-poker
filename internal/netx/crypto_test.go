package netx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func randomSecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, KeySize)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func TestDeriveSessionBothSides(t *testing.T) {
	alice, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// A JSON round trip proves the identity survives storage.
	var aliceRound Identity
	raw, _ := json.Marshal(alice)
	if err := json.Unmarshal(raw, &aliceRound); err != nil {
		t.Fatal(err)
	}
	if aliceRound != alice {
		t.Fatalf("identity did not round-trip: %+v", aliceRound)
	}
	secret := randomSecret(t)

	left, err := DeriveSession(alice.Priv, bob.Pub, secret, "t_1", "p_a")
	if err != nil {
		t.Fatal(err)
	}
	right, err := DeriveSession(bob.Priv, alice.Pub, secret, "t_1", "p_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 32 {
		t.Fatalf("session length = %d", len(left))
	}
	if !SafeEqual(string(left), string(right)) {
		t.Fatal("both sides must derive the same session")
	}

	otherPlayer, err := DeriveSession(alice.Priv, bob.Pub, secret, "t_1", "p_b")
	if err != nil {
		t.Fatal(err)
	}
	if string(left) == string(otherPlayer) {
		t.Fatal("player id must be bound into the session")
	}
	otherTable, err := DeriveSession(alice.Priv, bob.Pub, secret, "t_2", "p_a")
	if err != nil {
		t.Fatal(err)
	}
	if string(left) == string(otherTable) {
		t.Fatal("table id must be bound into the session")
	}
	wrongSecret, err := DeriveSession(alice.Priv, bob.Pub, randomSecret(t), "t_1", "p_a")
	if err != nil {
		t.Fatal(err)
	}
	if string(left) == string(wrongSecret) {
		t.Fatal("table secret must be the HKDF salt")
	}

	if _, err := DeriveSession(alice.Priv, bob.Pub, secret, "", "p_a"); err == nil {
		t.Fatal("missing table id must fail")
	}
	if _, err := DeriveSession(alice.Priv, "not a key", secret, "t_1", "p_a"); err == nil {
		t.Fatal("bad peer key must fail")
	}
	if _, err := DeriveSession(alice.Priv, bob.Pub, randomSecret(t)[:10], "t_1", "p_a"); err == nil {
		t.Fatal("bad secret must fail")
	}
}

func TestJoinProofSensitive(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	secret := randomSecret(t)
	n := Nonce()
	proof := JoinProof(secret, "t_1", identity.Pub, n)
	if len(proof) != 64 || strings.Trim(proof, "0123456789abcdef") != "" {
		t.Fatalf("bad proof format %q", proof)
	}
	if proof != JoinProof(secret, "t_1", identity.Pub, n) {
		t.Fatal("proof must be deterministic")
	}
	other, _ := GenerateIdentity()
	cases := []string{
		JoinProof(randomSecret(t), "t_1", identity.Pub, n),
		JoinProof(secret, "t_2", identity.Pub, n),
		JoinProof(secret, "t_1", other.Pub, n),
		JoinProof(secret, "t_1", identity.Pub, Nonce()),
	}
	for i, got := range cases {
		if got == proof {
			t.Fatalf("case %d must change the proof", i)
		}
	}
	if JoinProof("bad secret", "t_1", identity.Pub, n) != "" {
		t.Fatal("invalid secret must yield an empty proof")
	}
}

func TestSignRequestRejectsTampering(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"raise","amount":300}`
	path := "/peer/action"
	ts := int64(1764000000000)
	sig := SignRequest(key, "post", path, ts, body)
	if len(sig) != 64 {
		t.Fatalf("bad signature format %q", sig)
	}
	if !VerifyRequest(key, "POST", path, ts, body, sig) {
		t.Fatal("valid signature rejected")
	}
	if !VerifyRequest(key, "post", path, ts, body, sig) {
		t.Fatal("method case must be normalized")
	}
	cases := []struct {
		name   string
		method string
		path   string
		ts     int64
		body   string
		other  []byte
	}{
		{"body appended", "POST", path, ts, body + " ", key},
		{"body changed", "POST", path, ts, `{"type":"raise","amount":301}`, key},
		{"method", "GET", path, ts, body, key},
		{"timestamp", "POST", path, ts + 1, body, key},
		{"path", "POST", "/peer/chat", ts, body, key},
		{"key", "POST", path, ts, body, make([]byte, 32)},
	}
	for _, tc := range cases {
		if VerifyRequest(tc.other, tc.method, tc.path, tc.ts, tc.body, sig) {
			t.Fatalf("%s must not verify", tc.name)
		}
	}
	if VerifyRequest(key, "POST", path, ts, body, sig+"0") {
		t.Fatal("longer signature accepted")
	}
	if VerifyRequest(key, "POST", path, ts, body, "") {
		t.Fatal("empty signature accepted")
	}
	empty := SignRequest(key, "GET", "/peer/events", ts, "")
	if !VerifyRequest(key, "GET", "/peer/events", ts, "", empty) {
		t.Fatal("empty body signature rejected")
	}
}

func TestNonceAndFingerprint(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint := Fingerprint(identity.Pub); fingerprint != Fingerprint(identity.Pub) {
		t.Fatal("fingerprint must be stable")
	}
	print := Fingerprint(identity.Pub)
	if len(print) != 16 || strings.Trim(print, "0123456789abcdef") != "" {
		t.Fatalf("bad fingerprint %q", print)
	}
	other, _ := GenerateIdentity()
	if print == Fingerprint(other.Pub) {
		t.Fatal("different keys must have different fingerprints")
	}
	if Fingerprint("nope") != "" {
		t.Fatal("invalid key must yield empty fingerprint")
	}
	seen := map[string]bool{}
	for range 50 {
		n := Nonce()
		if len(n) != 32 || strings.Trim(n, "0123456789abcdef") != "" {
			t.Fatalf("bad nonce %q", n)
		}
		if seen[n] {
			t.Fatal("nonce repeated")
		}
		seen[n] = true
	}
}

func TestHKDFMatchesRFC5869Case1(t *testing.T) {
	// RFC 5869 test case 1, first 32 bytes.
	ikm := strings.Repeat("\x0b", 22)
	salt := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c}
	info := []byte{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9}
	want := "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf"
	got := hkdfSHA256([]byte(ikm), salt, info, 32)
	if hexEncode(got) != want {
		t.Fatalf("hkdf mismatch\n got %s\nwant %s", hexEncode(got), want)
	}
}

func hexEncode(raw []byte) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for _, v := range raw {
		b.WriteByte(digits[v>>4])
		b.WriteByte(digits[v&0x0f])
	}
	return b.String()
}

func TestHostProofSensitive(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	proof := HostProof(key, "p_1")
	if len(proof) != 64 || strings.Trim(proof, "0123456789abcdef") != "" {
		t.Fatalf("bad host proof %q", proof)
	}
	if proof != HostProof(key, "p_1") {
		t.Fatal("host proof must be deterministic")
	}
	other := make([]byte, 32)
	if proof == HostProof(other, "p_1") || proof == HostProof(key, "p_2") {
		t.Fatal("host proof must bind key and player")
	}
}
