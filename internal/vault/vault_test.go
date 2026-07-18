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
	saved, err := store.PutForUser(key, "admin", Credential{Name: "prod", Host: "example.com", Username: "root", PrivateKey: "secret-key", Passphrase: "secret-pass"})
	if err != nil {
		t.Fatal(err)
	}
	summaries, removed, err := store.ListSummaries("admin", bytes.Repeat([]byte{8}, 32))
	if err != nil || removed != 0 || len(summaries) != 1 || summaries[0].Name != "prod" || !summaries[0].HasPrivateKey || !summaries[0].HasPassphrase {
		t.Fatalf("summaries=%+v removed=%d err=%v", summaries, removed, err)
	}
	otherSummaries, removed, err := store.ListSummaries("other", bytes.Repeat([]byte{8}, 32))
	if err != nil || removed != 0 || len(otherSummaries) != 0 {
		t.Fatalf("other summaries=%+v removed=%d err=%v", otherSummaries, removed, err)
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

func TestListSummariesRemovesUnmigratableLegacyCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldKey := bytes.Repeat([]byte{7}, 32)
	if _, err := store.Put(oldKey, Credential{Name: "old", Host: "example.com", Username: "root", PrivateKey: "secret-key"}); err != nil {
		t.Fatal(err)
	}
	summaries, removed, err := store.ListSummaries("admin", bytes.Repeat([]byte{8}, 32))
	if err != nil || removed != 1 || len(summaries) != 0 {
		t.Fatalf("summaries=%+v removed=%d err=%v", summaries, removed, err)
	}
	items, err := store.List(oldKey)
	if err != nil || len(items) != 0 {
		t.Fatalf("legacy credential should be removed, items=%+v err=%v", items, err)
	}
}
