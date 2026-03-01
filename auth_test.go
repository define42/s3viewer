package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/securecookie"
)

func TestNewSecureCookieFromEnvInvalidHashKey(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "short")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "1234567890123456")
	if _, err := newSecureCookieFromEnv(); err == nil {
		t.Fatalf("expected error for short hash key")
	}
}

func TestNewSecureCookieFromEnvSuccess(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")
	sc, err := newSecureCookieFromEnv()
	if err != nil {
		t.Fatalf("expected secure cookie config success, got: %v", err)
	}
	if sc == nil {
		t.Fatalf("expected non-nil secure cookie")
	}
}

func TestNewSecureCookieFromEnvInvalidBlockKey(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "short")
	if _, err := newSecureCookieFromEnv(); err == nil {
		t.Fatalf("expected error for invalid block key length")
	}
}

func TestNewSecureCookieFromEnvGeneratesKeysWhenUnset(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "")
	sc, err := newSecureCookieFromEnv()
	if err != nil {
		t.Fatalf("expected secure cookie generation success, got: %v", err)
	}
	if sc == nil {
		t.Fatalf("expected non-nil secure cookie")
	}
}

func TestRandomKey(t *testing.T) {
	key, err := randomKey(32)
	if err != nil {
		t.Fatalf("randomKey returned error: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
}

func TestGenerateRGWToken(t *testing.T) {
	token, err := generateRGWToken("user1", "pass1")
	if err != nil {
		t.Fatalf("generateRGWToken returned error: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatalf("expected non-empty token")
	}

	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not valid base64: %v", err)
	}

	var payload struct {
		RGWToken struct {
			Version int    `json:"version"`
			Type    string `json:"type"`
			ID      string `json:"id"`
			Key     string `json:"key"`
		} `json:"RGW_TOKEN"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("token payload is not valid JSON: %v", err)
	}

	if payload.RGWToken.Version != 1 {
		t.Fatalf("expected version=1, got %d", payload.RGWToken.Version)
	}
	if payload.RGWToken.Type != "ad" {
		t.Fatalf("expected type=ad, got %q", payload.RGWToken.Type)
	}
	if payload.RGWToken.ID != "user1" {
		t.Fatalf("expected id=user1, got %q", payload.RGWToken.ID)
	}
	if payload.RGWToken.Key != "pass1" {
		t.Fatalf("expected key=pass1, got %q", payload.RGWToken.Key)
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	a := newAuthUnitTestApp()

	setReq := httptest.NewRequest(http.MethodGet, "/", nil)
	setRec := httptest.NewRecorder()
	if err := a.setSessionCookie(setRec, setReq, userSession{AccessKey: "ak", SecretKey: "sk"}); err != nil {
		t.Fatalf("set session cookie failed: %v", err)
	}

	cookies := setRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie in response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		getReq.AddCookie(c)
	}
	sess, err := a.getSession(getReq)
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if sess.AccessKey != "ak" || sess.SecretKey != "sk" {
		t.Fatalf("unexpected session: %#v", sess)
	}

	clearRec := httptest.NewRecorder()
	a.clearSessionCookie(clearRec, getReq)
	cleared := clearRec.Result().Cookies()
	if len(cleared) == 0 || cleared[0].MaxAge >= 0 {
		t.Fatalf("expected clearing session cookie with MaxAge < 0")
	}
}

func TestRequireSessionRedirectsWithoutCookie(t *testing.T) {
	a := newAuthUnitTestApp()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	_, ok := a.requireSession(rec, req)
	if ok {
		t.Fatalf("expected requireSession to fail without cookie")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("expected redirect to /login, got %q", got)
	}
}

func TestHandleLoginGETRendersForm(t *testing.T) {
	a := newAuthUnitTestApp()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Access Key") {
		t.Fatalf("expected login form in response")
	}
}

func TestHandleLoginGETWithSessionRedirectsHome(t *testing.T) {
	a := newAuthUnitTestApp()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/" {
		t.Fatalf("expected redirect to /, got %q", got)
	}
}

func TestHandleLoginPOSTMissingFields(t *testing.T) {
	a := newAuthUnitTestApp()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("access_key="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "required") {
		t.Fatalf("expected validation message in response")
	}
}

func TestHandleLoginPOSTClientInitFailure(t *testing.T) {
	a := newAuthUnitTestApp()
	a.endpoint = "::://bad-endpoint"

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("access_key=ak&secret_key=sk"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with rendered login form, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "failed to create S3 client") {
		t.Fatalf("expected client creation failure message")
	}
}

func TestHandleLoginMethodNotAllowed(t *testing.T) {
	a := newAuthUnitTestApp()
	req := httptest.NewRequest(http.MethodPut, "/login", nil)
	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleLogout(t *testing.T) {
	a := newAuthUnitTestApp()

	nonPostReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	nonPostRec := httptest.NewRecorder()
	a.handleLogout(nonPostRec, nonPostReq)
	if nonPostRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET logout, got %d", nonPostRec.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	postRec := httptest.NewRecorder()
	a.handleLogout(postRec, postReq)
	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 for POST logout, got %d", postRec.Code)
	}
	if got := postRec.Header().Get("Location"); got != "/login" {
		t.Fatalf("expected redirect to /login, got %q", got)
	}
}

func TestAuthenticatedS3ClientFailure(t *testing.T) {
	a := newAuthUnitTestApp()
	a.endpoint = "::://bad-endpoint"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	addSessionCookieToRequest(t, a, req)
	rec := httptest.NewRecorder()

	client, ok := a.authenticatedS3Client(rec, req)
	if ok || client != nil {
		t.Fatalf("expected authenticatedS3Client to fail")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 status, got %d", rec.Code)
	}
}

func newAuthUnitTestApp() *app {
	hashKey := []byte("0123456789abcdef0123456789abcdef")
	blockKey := []byte("abcdef0123456789abcdef0123456789")
	cookie := securecookie.New(hashKey, blockKey)
	cookie.MaxAge(int(sessionTTL.Seconds()))

	return &app{
		tpl:            newTemplates(),
		region:         "us-east-1",
		endpoint:       "http://localhost:9000",
		forcePathStyle: true,
		cookieName:     sessionCookieName,
		cookie:         cookie,
	}
}
