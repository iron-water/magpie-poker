// Package netx implements the peer crypto, invite code and handshake helpers
// of docs/PROTOCOL.md. Keys and secrets are transported as base64url
// (unpadded URL-safe base64) of 32 raw bytes.
package netx

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the size of every X25519 key and table secret.
const KeySize = 32

// Identity is an X25519 key pair. Pub and Priv are unpadded base64url
// encodings of the 32 raw bytes.
type Identity struct {
	Pub  string `json:"pub"`
	Priv string `json:"priv"`
}

// SafeEqual compares two strings in constant time.
func SafeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func encodeKey(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeKey parses a base64url encoded 32-byte value. Padding is tolerated,
// matching Buffer.from(text, "base64url") in the JS implementation.
func decodeKey(value, label string) ([]byte, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil, fmt.Errorf("%s 不能为空", label)
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(text)
	}
	if err != nil {
		return nil, fmt.Errorf("%s 不是合法的 base64url", label)
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("%s 必须是 32 字节", label)
	}
	return raw, nil
}

// DecodeKey parses a 32-byte base64url value.
func DecodeKey(value, label string) ([]byte, error) { return decodeKey(value, label) }

// EncodeKey encodes raw bytes as base64url.
func EncodeKey(raw []byte) string { return encodeKey(raw) }

// GenerateIdentity creates a fresh X25519 key pair.
func GenerateIdentity() (Identity, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	pub := priv.PublicKey()
	return Identity{Pub: encodeKey(pub.Bytes()), Priv: encodeKey(priv.Bytes())}, nil
}

func privateKey(priv string) (*ecdh.PrivateKey, error) {
	raw, err := decodeKey(priv, "私钥")
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// PublicKey parses a base64url X25519 public key.
func PublicKey(pub string) (*ecdh.PublicKey, error) {
	raw, err := decodeKey(pub, "公钥")
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPublicKey(raw)
}

// DeriveSession performs X25519 and HKDF-SHA256 with the table secret as
// salt and "poker1|tableId|playerId" as info, returning 32 bytes.
func DeriveSession(priv, peerPub, tableSecret, tableID, playerID string) ([]byte, error) {
	if strings.TrimSpace(tableID) == "" {
		return nil, errors.New("缺少牌桌 id")
	}
	if strings.TrimSpace(playerID) == "" {
		return nil, errors.New("缺少玩家 id")
	}
	own, err := privateKey(priv)
	if err != nil {
		return nil, err
	}
	peer, err := PublicKey(peerPub)
	if err != nil {
		return nil, err
	}
	shared, err := own.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("X25519 失败: %w", err)
	}
	salt, err := decodeKey(tableSecret, "牌桌密钥")
	if err != nil {
		return nil, err
	}
	info := []byte("poker1|" + tableID + "|" + playerID)
	return hkdfSHA256(shared, salt, info, KeySize), nil
}

// hkdfSHA256 is HKDF (RFC 5869) with SHA-256.
func hkdfSHA256(secret, salt, info []byte, length int) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	extract := hmac.New(sha256.New, salt)
	extract.Write(secret)
	prk := extract.Sum(nil)

	out := make([]byte, 0, length)
	var block []byte
	for counter := byte(1); len(out) < length; counter++ {
		expand := hmac.New(sha256.New, prk)
		expand.Write(block)
		expand.Write(info)
		expand.Write([]byte{counter})
		block = expand.Sum(nil)
		out = append(out, block...)
	}
	return out[:length]
}

func hmacHex(key []byte, message string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// JoinProof returns hex(HMAC-SHA256(secret, "join1|tableId|pub|nonce")).
// It returns "" if the secret is not a valid 32-byte key.
func JoinProof(tableSecret, tableID, pub, nonce string) string {
	key, err := decodeKey(tableSecret, "牌桌密钥")
	if err != nil {
		return ""
	}
	return hmacHex(key, "join1|"+tableID+"|"+pub+"|"+nonce)
}

// HostProof returns hex(HMAC-SHA256(session, "host1|playerId")).
func HostProof(session []byte, playerID string) string {
	return hmacHex(session, "host1|"+playerID)
}

// SignRequest returns the hex signature of
// METHOD \n path \n ts \n sha256hex(body).
func SignRequest(session []byte, method, path string, ts int64, body string) string {
	bodySum := sha256.Sum256([]byte(body))
	text := strings.ToUpper(method) + "\n" + path + "\n" + fmt.Sprint(ts) + "\n" + hex.EncodeToString(bodySum[:])
	return hmacHex(session, text)
}

// VerifyRequest checks a signature produced by SignRequest in constant time.
func VerifyRequest(session []byte, method, path string, ts int64, body, sig string) bool {
	if sig == "" {
		return false
	}
	expected := SignRequest(session, method, path, ts, body)
	return SafeEqual(expected, sig)
}

// Nonce returns 16 random bytes as 32 hex characters.
func Nonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("netx: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// Fingerprint returns the first 8 bytes of sha256(pub) as hex, or "" when the
// key cannot be parsed.
func Fingerprint(pub string) string {
	raw, err := decodeKey(pub, "公钥")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
