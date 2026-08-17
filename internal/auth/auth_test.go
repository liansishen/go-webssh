package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginLogoutSession(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(time.Hour)
	a := &Authenticator{
		Username:      "admin",
		PasswordHash:  string(hash),
		Store:         store,
		TTL:           time.Hour,
		SessionSecret: "test-session-secret",
	}

	rr := httptest.NewRecorder()
	if err := a.Login(rr, "admin", "wrong"); err == nil {
		t.Fatal("expected auth failure")
	}
	if err := a.Login(rr, "admin", "s3cret"); err != nil {
		t.Fatalf("login: %v", err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	c := cookies[0]
	if c.Name != CookieName || !c.HttpOnly {
		t.Fatalf("cookie=%+v", c)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	info, ok := a.SessionFromRequest(req)
	if !ok || info.Username != "admin" {
		t.Fatalf("session=%+v ok=%v", info, ok)
	}

	tampered := *c
	tampered.Value += "x"
	tamperedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	tamperedReq.AddCookie(&tampered)
	if _, ok := a.SessionFromRequest(tamperedReq); ok {
		t.Fatal("tampered session cookie must be rejected")
	}

	rr2 := httptest.NewRecorder()
	a.Logout(rr2, req)
	if _, ok := a.SessionFromRequest(req); ok {
		t.Fatal("session should be deleted")
	}
}

func TestPlaintextPassword(t *testing.T) {
	a := &Authenticator{
		Username: "admin",
		Password: "plain",
		Store:    NewStore(time.Hour),
		TTL:      time.Hour,
	}
	if err := a.Authenticate("admin", "plain"); err != nil {
		t.Fatal(err)
	}
	if err := a.Authenticate("admin", "nope"); err == nil {
		t.Fatal("expected failure")
	}
}

func TestRememberLoginLastsThirtyDays(t *testing.T) {
	a := &Authenticator{Username: "admin", Password: "plain", Store: NewStore(time.Hour), TTL: time.Hour, SessionSecret: "test-session-secret"}
	rr := httptest.NewRecorder()
	if err := a.Login(rr, "admin", "plain", true); err != nil {
		t.Fatal(err)
	}
	remaining := time.Until(rr.Result().Cookies()[0].Expires)
	if remaining < 29*24*time.Hour || remaining > 30*24*time.Hour+time.Minute {
		t.Fatalf("remembered cookie lifetime=%s", remaining)
	}

	restarted := &Authenticator{Username: "admin", Password: "changed", Store: NewStore(time.Hour), TTL: time.Hour, SessionSecret: "test-session-secret"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	info, ok := restarted.SessionFromRequest(req)
	if !ok || info.Username != "admin" {
		t.Fatalf("persistent session after restart=%+v ok=%v", info, ok)
	}

	tampered := *rr.Result().Cookies()[0]
	tampered.Value += "x"
	tamperedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	tamperedReq.AddCookie(&tampered)
	if _, ok := restarted.SessionFromRequest(tamperedReq); ok {
		t.Fatal("tampered persistent cookie must be rejected")
	}
}
