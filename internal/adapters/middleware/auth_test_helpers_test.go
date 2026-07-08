package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// HMAC-SHA256 of value with secret, encoded as hex / base64-raw-url-no-padding /
// base64-std-with-padding. These helpers live in the test file so production
// code is not bloated with one-off crypto utilities — they exist solely to
// satisfy TestVerifySessionHMAC*.

func signCookieHex(t *testing.T, value, secret string) (raw []byte, sig string) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	raw = mac.Sum(nil)
	sig = hex.EncodeToString(raw)
	return raw, sig
}

func signCookieBase64Raw(t *testing.T, value, secret string) (raw []byte, sig string) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	raw = mac.Sum(nil)
	sig = base64.RawURLEncoding.EncodeToString(raw)
	return raw, sig
}

func signCookieBase64Std(t *testing.T, value, secret string) (raw []byte, sig string) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	raw = mac.Sum(nil)
	sig = base64.StdEncoding.EncodeToString(raw)
	return raw, sig
}

func flipFirstHexByte(sig string) string {
	if len(sig) < 2 {
		return sig
	}
	// XOR the first hex chroma with 0xff to produce a guaranteed-different
	// byte without any string-table lookup.
	s := []byte(sig)
	if s[0] == '0' {
		s[0] = 'f'
	} else {
		s[0] = '0'
	}
	return string(s)
}
