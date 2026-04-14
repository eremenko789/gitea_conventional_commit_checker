package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// verifyGiteaSignature checks X-Gitea-Signature (hex HMAC-SHA256) or X-Hub-Signature-256 (sha256=<hex>).
func verifyGiteaSignature(secret, body []byte, giteaSig, hubSig256 string) bool {
	if len(secret) == 0 {
		return true
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	sum := hex.EncodeToString(mac.Sum(nil))
	if giteaSig != "" && subtle.ConstantTimeCompare([]byte(strings.ToLower(sum)), []byte(strings.ToLower(strings.TrimSpace(giteaSig)))) == 1 {
		return true
	}
	hubSig256 = strings.TrimSpace(hubSig256)
	if strings.HasPrefix(strings.ToLower(hubSig256), "sha256=") {
		hexPart := strings.TrimSpace(hubSig256[7:])
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(sum)), []byte(strings.ToLower(hexPart))) == 1 {
			return true
		}
	}
	return false
}
