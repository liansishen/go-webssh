package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedCredentialRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := bytes.Repeat([]byte{7}, 32)
	saved, err := store.Put(key, Credential{Name: "prod", Host: "example.com", Username: "root", PrivateKey: "secret-key", Passphrase: "secret-pass"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(key)
	if err != nil || len(items) != 1 || items[0].PrivateKey != "secret-key" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-key")) || bytes.Contains(raw, []byte("secret-pass")) {
		t.Fatal("database contains plaintext secret")
	}
	if _, err := store.List(bytes.Repeat([]byte{8}, 32)); err == nil {
		t.Fatal("wrong key must not decrypt credentials")
	}
	if err := store.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	items, err = store.List(key)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
