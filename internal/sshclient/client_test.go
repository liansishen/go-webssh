package sshclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestParseSignerEd25519OrECDSA(t *testing.T) {
	// Generate ECDSA key (widely available) and parse via ssh.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	signer, err := ParseSigner(string(pemBytes), "")
	if err != nil {
		t.Fatal(err)
	}
	if signer.PublicKey() == nil {
		t.Fatal("nil public key")
	}
}

func TestParseSignerInvalid(t *testing.T) {
	_, err := ParseSigner("not-a-key", "")
	if err == nil {
		t.Fatal("expected error")
	}
	ce, ok := err.(*CodedError)
	if !ok || ce.Code != "PRIVATE_KEY_INVALID" {
		t.Fatalf("err=%v", err)
	}
}

func TestParseSignerPassphraseRequired(t *testing.T) {
	// Create encrypted private key using ssh marshal with passphrase via raw PEM encryption is hard;
	// instead ensure empty key path maps correctly for missing passphrase on encrypted keys by using
	// ssh.MarshalPrivateKeyWithPassphrase if available.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(key, "", []byte("pass"))
	if err != nil {
		t.Skip("MarshalPrivateKeyWithPassphrase not available:", err)
	}
	pemBytes := pem.EncodeToMemory(block)
	_, err = ParseSigner(string(pemBytes), "")
	if err == nil {
		t.Fatal("expected passphrase required")
	}
	ce, ok := err.(*CodedError)
	if !ok || ce.Code != "PRIVATE_KEY_PASSPHRASE_REQUIRED" {
		t.Fatalf("err=%v code=%v", err, ce)
	}
	_, err = ParseSigner(string(pemBytes), "wrong")
	if err == nil {
		t.Fatal("expected invalid passphrase")
	}
	_, err = ParseSigner(string(pemBytes), "pass")
	if err != nil {
		t.Fatal(err)
	}
}
