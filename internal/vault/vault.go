package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/argon2"
)

var (
	metaBucket           = []byte("meta")
	credentialBucket     = []byte("credentials")
	summaryBucket        = []byte("credential_summaries")
	saltKey              = []byte("vault_salt")
	masterFingerprintKey = []byte("master_key_fingerprint")
	v2Prefix             = []byte("GWS2")
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
	UseHerdr   bool      `json:"useHerdr"`
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
	UseHerdr      bool      `json:"useHerdr"`
	HasPrivateKey bool      `json:"hasPrivateKey"`
	HasPassphrase bool      `json:"hasPassphrase"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (c *Credential) UnmarshalJSON(data []byte) error {
	type credentialAlias Credential
	decoded := struct {
		*credentialAlias
		LegacyUseTmux bool `json:"useTmux"`
	}{credentialAlias: (*credentialAlias)(c)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	c.UseHerdr = c.UseHerdr || decoded.LegacyUseTmux
	return nil
}

func (s *Summary) UnmarshalJSON(data []byte) error {
	type summaryAlias Summary
	decoded := struct {
		*summaryAlias
		LegacyUseTmux bool `json:"useTmux"`
	}{summaryAlias: (*summaryAlias)(s)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	s.UseHerdr = s.UseHerdr || decoded.LegacyUseTmux
	return nil
}

func (c Credential) MarshalJSON() ([]byte, error) {
	type credentialAlias Credential
	return json.Marshal(struct {
		credentialAlias
		LegacyUseTmux bool `json:"useTmux"`
	}{credentialAlias: credentialAlias(c), LegacyUseTmux: c.UseHerdr})
}

func (s Summary) MarshalJSON() ([]byte, error) {
	type summaryAlias Summary
	return json.Marshal(struct {
		summaryAlias
		LegacyUseTmux bool `json:"useTmux"`
	}{summaryAlias: summaryAlias(s), LegacyUseTmux: s.UseHerdr})
}

type OpenOptions struct {
	KeyFile string
	KeyHex  string
}

type Store struct {
	db        *bolt.DB
	salt      []byte
	masterKey []byte
}

func Open(path string, options ...OpenOptions) (*Store, error) {
	opts := OpenOptions{KeyFile: path + ".key"}
	if len(options) > 0 {
		opts = options[0]
		if strings.TrimSpace(opts.KeyFile) == "" {
			opts.KeyFile = path + ".key"
		}
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open credential database: %w", err)
	}
	s := &Store{db: db}
	var storedFingerprint []byte
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
		storedFingerprint = append([]byte(nil), meta.Get(masterFingerprintKey)...)
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize credential database: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure credential database: %w", err)
	}

	key, err := loadMasterKey(opts, len(storedFingerprint) == 0)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	fingerprint := sha256.Sum256(key)
	if len(storedFingerprint) > 0 && !bytes.Equal(storedFingerprint, fingerprint[:]) {
		zero(key)
		_ = db.Close()
		return nil, errors.New("credential master key does not match this database; restore the matching key file")
	}
	if len(storedFingerprint) == 0 {
		if err := db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket(metaBucket).Put(masterFingerprintKey, fingerprint[:])
		}); err != nil {
			zero(key)
			_ = db.Close()
			return nil, fmt.Errorf("store credential master key fingerprint: %w", err)
		}
	}
	s.masterKey = key
	return s, nil
}

func loadMasterKey(opts OpenOptions, allowCreate bool) ([]byte, error) {
	if strings.TrimSpace(opts.KeyHex) != "" {
		key, err := decodeMasterKey(opts.KeyHex)
		if err != nil {
			return nil, fmt.Errorf("invalid credential master key: %w", err)
		}
		return key, nil
	}

	info, err := os.Lstat(opts.KeyFile)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("credential master key file must not be a symbolic link")
		}
		raw, err := os.ReadFile(opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("read credential master key: %w", err)
		}
		if err := os.Chmod(opts.KeyFile, 0o600); err != nil {
			return nil, fmt.Errorf("secure credential master key: %w", err)
		}
		key, err := decodeMasterKey(string(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid credential master key file: %w", err)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect credential master key: %w", err)
	}
	if !allowCreate {
		return nil, errors.New("credential master key file is missing; restore it with the credential database")
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate credential master key: %w", err)
	}
	file, err := os.OpenFile(opts.KeyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		zero(key)
		return nil, fmt.Errorf("create credential master key: %w", err)
	}
	encoded := []byte(hex.EncodeToString(key) + "\n")
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(opts.KeyFile)
		zero(key)
		return nil, fmt.Errorf("write credential master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(opts.KeyFile)
		zero(key)
		return nil, fmt.Errorf("sync credential master key: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(opts.KeyFile)
		zero(key)
		return nil, fmt.Errorf("close credential master key: %w", err)
	}
	return key, nil
}

func decodeMasterKey(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("key must be exactly 64 hexadecimal characters")
	}
	return decoded, nil
}

func (s *Store) Close() error {
	zero(s.masterKey)
	zero(s.salt)
	return s.db.Close()
}

func (s *Store) Put(credential Credential) (Credential, error) {
	return s.PutForUser("", credential)
}

func (s *Store) PutForUser(owner string, credential Credential) (Credential, error) {
	now := time.Now().UTC()
	if credential.ID == "" {
		credential.ID = randomID()
		credential.CreatedAt = now
	} else if credential.CreatedAt.IsZero() {
		if existing, err := s.Get(credential.ID); err == nil {
			credential.CreatedAt = existing.CreatedAt
		}
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
	sealed, err := encryptV2(s.masterKey, plaintext, []byte(credential.ID))
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
		UseHerdr:      c.UseHerdr,
		HasPrivateKey: c.PrivateKey != "",
		HasPassphrase: c.Passphrase != "",
		UpdatedAt:     c.UpdatedAt,
	}
}

func (s *Store) List() ([]Credential, error) {
	result, skipped, err := s.ListAvailable()
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		return nil, errors.New("unable to decrypt credential database")
	}
	return result, nil
}

func (s *Store) ListAvailable() ([]Credential, int, error) {
	var result []Credential
	skipped := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			plaintext, err := s.decryptRecord(sealed, id)
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

func (s *Store) ListSummaries(owner string) ([]Summary, int, error) {
	if owner == "" {
		return nil, 0, errors.New("credential owner is required")
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
	legacyCount := 0
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			if !isV2(sealed) {
				legacyCount++
			}
			if _, ok := knownSummaryIDs[string(id)]; ok {
				return nil
			}
			plaintext, err := s.decryptRecord(sealed, id)
			if err != nil {
				return nil
			}
			defer zero(plaintext)
			var credential Credential
			if err := json.Unmarshal(plaintext, &credential); err != nil {
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

	result := make([]Summary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, legacyCount, nil
}

func (s *Store) Get(id string) (Credential, error) {
	var credential Credential
	err := s.db.View(func(tx *bolt.Tx) error {
		sealed := tx.Bucket(credentialBucket).Get([]byte(id))
		if sealed == nil {
			return os.ErrNotExist
		}
		plaintext, err := s.decryptRecord(sealed, []byte(id))
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

func (s *Store) MigrateLegacyFromPassword(password []byte) (int, int, error) {
	if len(password) == 0 {
		return 0, 0, errors.New("legacy credential password is required")
	}
	legacyKey := argon2.IDKey(password, s.salt, 3, 64*1024, 2, 32)
	defer zero(legacyKey)

	type migratedRecord struct {
		id     []byte
		sealed []byte
	}
	var records []migratedRecord
	remaining := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(id, sealed []byte) error {
			if isV2(sealed) {
				return nil
			}
			plaintext, err := decrypt(legacyKey, sealed, id)
			if err != nil {
				remaining++
				return nil
			}
			var credential Credential
			if err := json.Unmarshal(plaintext, &credential); err != nil {
				zero(plaintext)
				remaining++
				return nil
			}
			newSealed, err := encryptV2(s.masterKey, plaintext, id)
			zero(plaintext)
			if err != nil {
				return err
			}
			records = append(records, migratedRecord{id: append([]byte(nil), id...), sealed: newSealed})
			return nil
		})
	})
	if err != nil {
		return 0, remaining, err
	}
	if len(records) > 0 {
		if err := s.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(credentialBucket)
			for _, record := range records {
				if err := bucket.Put(record.id, record.sealed); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return 0, remaining + len(records), err
		}
	}
	return len(records), remaining, nil
}

func (s *Store) LegacyCount() (int, error) {
	count := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(credentialBucket).ForEach(func(_, sealed []byte) error {
			if !isV2(sealed) {
				count++
			}
			return nil
		})
	})
	return count, err
}

func (s *Store) decryptRecord(sealed, aad []byte) ([]byte, error) {
	if !isV2(sealed) {
		return nil, errors.New("legacy credential requires migration")
	}
	return decrypt(s.masterKey, sealed[len(v2Prefix):], aad)
}

func isV2(sealed []byte) bool {
	return len(sealed) > len(v2Prefix) && bytes.Equal(sealed[:len(v2Prefix)], v2Prefix)
}

func encryptV2(key, plaintext, aad []byte) ([]byte, error) {
	sealed, err := encrypt(key, plaintext, aad)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), v2Prefix...), sealed...), nil
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
