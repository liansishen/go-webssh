package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	metaBucket       = []byte("meta")
	credentialBucket = []byte("credentials")
	summaryBucket    = []byte("credential_summaries")
	saltKey          = []byte("vault_salt")
)

type Credential struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	PrivateKey string    `json:"privateKey"`
	Passphrase string    `json:"passphrase,omitempty"`
	Term       string    `json:"term"`
	UseTmux    bool      `json:"useTmux"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Summary struct {
	ID            string    `json:"id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Username      string    `json:"username"`
	Term          string    `json:"term"`
	UseTmux       bool      `json:"useTmux"`
	HasPrivateKey bool      `json:"hasPrivateKey"`
	HasPassphrase bool      `json:"hasPassphrase"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Store struct {
	db   *bolt.DB
	salt []byte
}

func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open credential database: %w", err)
	}
	s := &Store{db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(credentialBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(summaryBucket); err != nil {
			return err
		}
		salt := meta.Get(saltKey)
		if len(salt) == 0 {
			salt = make([]byte, 16)
			if _, err := rand.Read(salt); err != nil {
				return err
			}
			if err := meta.Put(saltKey, salt); err != nil {
				return err
			}
		}
		s.salt = append([]byte(nil), salt...)
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize credential database: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure credential database: %w", err)
	}
	return s, nil
}

func (s *Store) Salt() []byte { return append([]byte(nil), s.salt...) }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Put(key []byte, credential Credential) (Credential, error) {
	return s.PutForUser(key, "", credential)
}

func (s *Store) PutForUser(key []byte, owner string, credential Credential) (Credential, error) {
	if len(key) != 32 {
		return Credential{}, errors.New("vault is locked")
	}
	now := time.Now().UTC()
	if credential.ID == "" {
		credential.ID = randomID()
		credential.CreatedAt = now
	}
	credential.UpdatedAt = now
	if credential.Port == 0 {
		credential.Port = 22
	}
	if credential.Term == "" {
		credential.Term = "xterm-256color"
	}
	plaintext, err := json.Marshal(credential)
	if err != nil {
		return Credential{}, err
	}
	defer zero(plaintext)
	sealed, err := encrypt(key, plaintext, []byte(credential.ID))
	if err != nil {
		return Credential{}, err
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		id := []byte(credential.ID)
		if err := tx.Bucket(credentialBucket).Put(id, sealed); err != nil {
			return err
		}
		if owner == "" {
			return nil
		}
		summary, err := json.Marshal(credential.Summary(owner))
		if err != nil {
			return err
		}
		return tx.Bucket(summaryBucket).Put(id, summary)
	})
	return credential, err
}

func (c Credential) Summary(owner string) Summary {
	return Summary{
		ID:            c.ID,
		Owner:         owner,
		Name:          c.Name,
		Host:          c.Host,
		Port:          c.Port,
		Username:      c.Username,
		Term:          c.Term,
		UseTmux:       c.UseTmux,
		HasPrivateKey: c.PrivateKey != "",
		HasPassphrase: c.Passphrase != "",
		UpdatedAt:     c.UpdatedAt,
	}
}

func (s *Store) List(key []byte) ([]Credential, error) {
	result, skipped, err := s.ListAvailable(key)
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		return nil, errors.New("unable to decrypt credential database")
	}
	return result, nil
}

func (s *Store) ListAvailable(key []byte) ([]Credential, int, error) {
	if len(key) != 32 {
		return nil, 0, errors.New("vault is locked")
	}
	var result []Credential
	skipped := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			plaintext, err := decrypt(key, sealed, id)
			if err != nil {
				skipped++
				return nil
			}
			defer zero(plaintext)
			var credential Credential
			if err := json.Unmarshal(plaintext, &credential); err != nil {
				skipped++
				return nil
			}
			result = append(result, credential)
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, skipped, err
}

func (s *Store) ListSummaries(owner string, key []byte) ([]Summary, int, error) {
	if owner == "" {
		return nil, 0, errors.New("credential owner is required")
	}
	if len(key) != 32 {
		return nil, 0, errors.New("vault is locked")
	}
	summaries := make(map[string]Summary)
	knownSummaryIDs := make(map[string]struct{})
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(summaryBucket).ForEach(func(id, raw []byte) error {
			var summary Summary
			if err := json.Unmarshal(raw, &summary); err != nil {
				return nil
			}
			knownSummaryIDs[string(id)] = struct{}{}
			if summary.Owner == owner {
				summaries[string(id)] = summary
			}
			return nil
		})
	}); err != nil {
		return nil, 0, err
	}

	var backfill []Summary
	var pruneIDs [][]byte
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			if _, ok := knownSummaryIDs[string(id)]; ok {
				return nil
			}
			plaintext, err := decrypt(key, sealed, id)
			if err != nil {
				pruneIDs = append(pruneIDs, append([]byte(nil), id...))
				return nil
			}
			defer zero(plaintext)
			var credential Credential
			if err := json.Unmarshal(plaintext, &credential); err != nil {
				pruneIDs = append(pruneIDs, append([]byte(nil), id...))
				return nil
			}
			summary := credential.Summary(owner)
			summaries[credential.ID] = summary
			backfill = append(backfill, summary)
			return nil
		})
	}); err != nil {
		return nil, 0, err
	}
	if len(backfill) > 0 {
		if err := s.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(summaryBucket)
			for _, summary := range backfill {
				raw, err := json.Marshal(summary)
				if err != nil {
					return err
				}
				if err := bucket.Put([]byte(summary.ID), raw); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, 0, err
		}
	}
	if len(pruneIDs) > 0 {
		if err := s.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(credentialBucket)
			for _, id := range pruneIDs {
				if err := bucket.Delete(id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, 0, err
		}
	}

	result := make([]Summary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, len(pruneIDs), nil
}

func (s *Store) Get(key []byte, id string) (Credential, error) {
	if len(key) != 32 {
		return Credential{}, errors.New("vault is locked")
	}
	var credential Credential
	err := s.db.View(func(tx *bolt.Tx) error {
		sealed := tx.Bucket(credentialBucket).Get([]byte(id))
		if sealed == nil {
			return os.ErrNotExist
		}
		plaintext, err := decrypt(key, sealed, []byte(id))
		if err != nil {
			return errors.New("unable to decrypt credential database")
		}
		defer zero(plaintext)
		return json.Unmarshal(plaintext, &credential)
	})
	return credential, err
}

func (s *Store) Delete(id string) error {
	if id == "" {
		return errors.New("credential id is required")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(credentialBucket).Delete([]byte(id)); err != nil {
			return err
		}
		return tx.Bucket(summaryBucket).Delete([]byte(id))
	})
}

func encrypt(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plaintext, aad)...), nil
}

func decrypt(key, sealed, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("invalid encrypted credential")
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], aad)
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
