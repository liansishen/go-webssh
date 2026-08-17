package vault

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/argon2"
)

func TestEncryptedCredentialRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.db")
	keyPath := path + ".key"
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.PutForUser("admin", Credential{Name: "prod", Host: "example.com", Username: "root", PrivateKey: "secret-key", Passphrase: "secret-pass", UseHerdr: true})
	if err != nil {
		t.Fatal(err)
	}
	summaries, legacy, err := store.ListSummaries("admin")
	if err != nil || legacy != 0 || len(summaries) != 1 || summaries[0].Name != "prod" || !summaries[0].UseHerdr || !summaries[0].HasPrivateKey || !summaries[0].HasPassphrase {
		t.Fatalf("summaries=%+v legacy=%d err=%v", summaries, legacy, err)
	}
	items, err := store.List()
	if err != nil || len(items) != 1 || items[0].PrivateKey != "secret-key" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-key")) || bytes.Contains(raw, []byte("secret-pass")) {
		t.Fatal("database contains plaintext secret")
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%o", info.Mode().Perm())
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(saved.ID)
	if err != nil || got.PrivateKey != "secret-key" {
		t.Fatalf("credential after reopen=%+v err=%v", got, err)
	}
	createdAt := got.CreatedAt
	got.Name = "prod-updated"
	got.CreatedAt = time.Time{}
	updated, err := reopened.PutForUser("admin", got)
	if err != nil || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("updated credential=%+v err=%v", updated, err)
	}
	if err := reopened.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRecoveryFlagCompatibility(t *testing.T) {
	var credential Credential
	if err := json.Unmarshal([]byte(`{"name":"old","useTmux":true}`), &credential); err != nil {
		t.Fatal(err)
	}
	if !credential.UseHerdr {
		t.Fatal("legacy useTmux flag was not mapped to Herdr")
	}
	raw, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"useHerdr":true`)) || !bytes.Contains(raw, []byte(`"useTmux":true`)) {
		t.Fatalf("compatibility fields missing: %s", raw)
	}
}

func TestMasterKeyMismatchFailsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	wrongKey := hex.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if _, err := Open(path, OpenOptions{KeyFile: path + ".key", KeyHex: wrongKey}); err == nil {
		t.Fatal("wrong master key must fail")
	}
	if err := os.Remove(path + ".key"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("missing master key must fail")
	}
}

func TestLegacyCredentialIsPreservedAndMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	password := []byte("earlier-password")
	legacyKey := argon2.IDKey(password, store.salt, 3, 64*1024, 2, 32)
	credential := Credential{ID: "legacy-id", Name: "old", Host: "example.com", Port: 22, Username: "root", PrivateKey: "secret-key", UseHerdr: true}
	plaintext, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypt(legacyKey, plaintext, []byte(credential.ID))
	zero(legacyKey)
	zero(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := json.Marshal(credential.Summary("admin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(credentialBucket).Put([]byte(credential.ID), sealed); err != nil {
			return err
		}
		return tx.Bucket(summaryBucket).Put([]byte(credential.ID), summary)
	}); err != nil {
		t.Fatal(err)
	}

	summaries, legacy, err := store.ListSummaries("admin")
	if err != nil || legacy != 1 || len(summaries) != 1 {
		t.Fatalf("summaries=%+v legacy=%d err=%v", summaries, legacy, err)
	}
	if _, err := store.Get(credential.ID); err == nil {
		t.Fatal("legacy credential must require migration")
	}
	migrated, remaining, err := store.MigrateLegacyFromPassword(password)
	if err != nil || migrated != 1 || remaining != 0 {
		t.Fatalf("migrated=%d remaining=%d err=%v", migrated, remaining, err)
	}
	got, err := store.Get(credential.ID)
	if err != nil || got.PrivateKey != "secret-key" || !got.UseHerdr {
		t.Fatalf("credential=%+v err=%v", got, err)
	}
}
