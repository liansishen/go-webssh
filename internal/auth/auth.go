package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const CookieName = "gowebssh_session"

const persistentTokenPrefix = "p1."

type persistentSession struct {
	Username string `json:"u"`
	Expires  int64  `json:"e"`
	VaultKey []byte `json:"k,omitempty"`
}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthorized       = errors.New("unauthorized")
)

type SessionInfo struct {
	Username  string
	ExpiresAt time.Time
	VaultKey  []byte
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]SessionInfo
	ttl      time.Duration
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	s := &Store{
		sessions: make(map[string]SessionInfo),
		ttl:      ttl,
	}
	go s.cleanupLoop()
	return s
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.sessions {
			if now.After(v.ExpiresAt) {
				zero(v.VaultKey)
				delete(s.sessions, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Store) Create(username string, vaultKey []byte, ttl ...time.Duration) (token string, expires time.Time, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	duration := s.ttl
	if len(ttl) > 0 && ttl[0] > 0 {
		duration = ttl[0]
	}
	expires = time.Now().Add(duration)
	s.mu.Lock()
	s.sessions[token] = SessionInfo{Username: username, ExpiresAt: expires, VaultKey: append([]byte(nil), vaultKey...)}
	s.mu.Unlock()
	return token, expires, nil
}

func (s *Store) Get(token string) (SessionInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.sessions[token]
	if !ok {
		return SessionInfo{}, false
	}
	if time.Now().After(info.ExpiresAt) {
		delete(s.sessions, token)
		return SessionInfo{}, false
	}
	info.VaultKey = append([]byte(nil), info.VaultKey...)
	return info, true
}

func (s *Store) Delete(token string) {
	s.mu.Lock()
	if info, ok := s.sessions[token]; ok {
		zero(info.VaultKey)
	}
	delete(s.sessions, token)
	s.mu.Unlock()
}

type Authenticator struct {
	Username      string
	Password      string
	PasswordHash  string
	Store         *Store
	SecureCookie  bool
	TTL           time.Duration
	SessionSecret string
	VaultSalt     []byte
}

func (a *Authenticator) Authenticate(username, password string) error {
	if subtle.ConstantTimeCompare([]byte(username), []byte(a.Username)) != 1 {
		// still compare password path to reduce timing differences a bit
		_ = a.checkPassword(password)
		return ErrInvalidCredentials
	}
	if err := a.checkPassword(password); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func (a *Authenticator) checkPassword(password string) error {
	if a.PasswordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password))
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.Password)) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func (a *Authenticator) Login(w http.ResponseWriter, username, password string, remember ...bool) error {
	if err := a.Authenticate(username, password); err != nil {
		return err
	}
	var vaultKey []byte
	if len(a.VaultSalt) > 0 {
		vaultKey = argon2.IDKey([]byte(password), a.VaultSalt, 3, 64*1024, 2, 32)
	}
	defer zero(vaultKey)
	var cookieValue string
	var expires time.Time
	var err error
	if len(remember) > 0 && remember[0] {
		expires = time.Now().Add(30 * 24 * time.Hour)
		cookieValue, err = a.sealPersistentSession(SessionInfo{Username: username, ExpiresAt: expires, VaultKey: vaultKey})
	} else {
		var token string
		token, expires, err = a.Store.Create(username, vaultKey, a.TTL)
		cookieValue = a.signedToken(token)
	}
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.SecureCookie,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
	return nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		if token, ok := a.verifyToken(c.Value); ok {
			a.Store.Delete(token)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.SecureCookie,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (a *Authenticator) SessionFromRequest(r *http.Request) (SessionInfo, bool) {
	info, ok := a.sessionFromRequest(r)
	if ok {
		zero(info.VaultKey)
		info.VaultKey = nil
	}
	return info, ok
}

func (a *Authenticator) VaultSessionFromRequest(r *http.Request) (SessionInfo, bool) {
	return a.sessionFromRequest(r)
}

func (a *Authenticator) sessionFromRequest(r *http.Request) (SessionInfo, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return SessionInfo{}, false
	}
	if strings.HasPrefix(c.Value, persistentTokenPrefix) {
		return a.openPersistentSession(c.Value)
	}
	token, ok := a.verifyToken(c.Value)
	if !ok {
		return SessionInfo{}, false
	}
	return a.Store.Get(token)
}

func (a *Authenticator) persistentAEAD() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("go-webssh/persistent-session/" + a.SessionSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (a *Authenticator) sealPersistentSession(info SessionInfo) (string, error) {
	aead, err := a.persistentAEAD()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(persistentSession{Username: info.Username, Expires: info.ExpiresAt.Unix(), VaultKey: info.VaultKey})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, payload, []byte(CookieName))
	return persistentTokenPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (a *Authenticator) openPersistentSession(value string) (SessionInfo, bool) {
	aead, err := a.persistentAEAD()
	if err != nil {
		return SessionInfo{}, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, persistentTokenPrefix))
	if err != nil || len(sealed) < aead.NonceSize() {
		return SessionInfo{}, false
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	payload, err := aead.Open(nil, nonce, ciphertext, []byte(CookieName))
	if err != nil {
		return SessionInfo{}, false
	}
	var session persistentSession
	if json.Unmarshal(payload, &session) != nil || session.Username != a.Username || session.Expires <= time.Now().Unix() {
		zero(session.VaultKey)
		return SessionInfo{}, false
	}
	return SessionInfo{Username: session.Username, ExpiresAt: time.Unix(session.Expires, 0), VaultKey: session.VaultKey}, true
}

func (a *Authenticator) signedToken(token string) string {
	mac := hmac.New(sha256.New, []byte(a.SessionSecret))
	_, _ = mac.Write([]byte(token))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return token + "." + sig
}

func (a *Authenticator) verifyToken(value string) (string, bool) {
	token, encodedSig, ok := strings.Cut(value, ".")
	if !ok || token == "" || encodedSig == "" {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(a.SessionSecret))
	_, _ = mac.Write([]byte(token))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", false
	}
	return token, true
}

func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.SessionFromRequest(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
