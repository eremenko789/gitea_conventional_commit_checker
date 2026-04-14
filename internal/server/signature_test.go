package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyGiteaSignature_headers(t *testing.T) {
	secret := []byte("s3cr3t")
	body := []byte(`{"action":"opened"}`)
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(body)
	sum := hex.EncodeToString(m.Sum(nil))

	if !verifyGiteaSignature(secret, body, sum, "") {
		t.Fatal("expected X-Gitea-Signature match")
	}
	if verifyGiteaSignature(secret, body, "00"+sum, "") {
		t.Fatal("expected mismatch")
	}
	hub := "sha256=" + sum
	if !verifyGiteaSignature(secret, body, "", hub) {
		t.Fatal("expected X-Hub-Signature-256 match")
	}
}
