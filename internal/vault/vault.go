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
		return tx.Bucket(credentialBucket).Put([]byte(credential.ID), sealed)
	})
	return credential, err
}

func (s *Store) List(key []byte) ([]Credential, error) {
	if len(key) != 32 {
		return nil, errors.New("vault is locked")
	}
	var result []Credential
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			plaintext, err := decrypt(key, sealed, id)
			if err != nil {
				return errors.New("unable to decrypt credential database")
			}
			defer zero(plaintext)
			var credential Credential
			if err := json.Unmarshal(plaintext, &credential); err != nil {
				return err
			}
			result = append(result, credential)
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
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
		return tx.Bucket(credentialBucket).Delete([]byte(id))
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
