package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gorilla/securecookie"
)

const (
	sessionCookieName = "s3viewer_session"
	sessionTTL        = 24 * time.Hour
)

type userSession struct {
	AccessKey string
	SecretKey string
}

func newSecureCookieFromEnv() (*securecookie.SecureCookie, error) {
	hashKey := []byte(getenv("SECURECOOKIE_HASH_KEY", ""))
	if len(hashKey) == 0 {
		var err error
		hashKey, err = randomKey(32)
		if err != nil {
			return nil, fmt.Errorf("generate hash key: %w", err)
		}
		log.Printf("warning: SECURECOOKIE_HASH_KEY not set; generated ephemeral key")
	}
	if len(hashKey) < 32 {
		return nil, fmt.Errorf("SECURECOOKIE_HASH_KEY must be at least 32 bytes")
	}

	blockKey := []byte(getenv("SECURECOOKIE_BLOCK_KEY", ""))
	if len(blockKey) == 0 {
		var err error
		blockKey, err = randomKey(32)
		if err != nil {
			return nil, fmt.Errorf("generate block key: %w", err)
		}
		log.Printf("warning: SECURECOOKIE_BLOCK_KEY not set; generated ephemeral key")
	}
	if l := len(blockKey); l != 16 && l != 24 && l != 32 {
		return nil, fmt.Errorf("SECURECOOKIE_BLOCK_KEY must be 16, 24, or 32 bytes")
	}

	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(int(sessionTTL.Seconds()))
	return sc, nil
}

func randomKey(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (a *app) setSessionCookie(w http.ResponseWriter, r *http.Request, sess userSession) error {
	value := map[string]string{
		"access_key": sess.AccessKey,
		"secret_key": sess.SecretKey,
	}

	encoded, err := a.cookie.Encode(a.cookieName, value)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func (a *app) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (a *app) getSession(r *http.Request) (userSession, error) {
	c, err := r.Cookie(a.cookieName)
	if err != nil {
		return userSession{}, err
	}

	var value map[string]string
	if err := a.cookie.Decode(a.cookieName, c.Value, &value); err != nil {
		return userSession{}, err
	}

	accessKey := strings.TrimSpace(value["access_key"])
	secretKey := strings.TrimSpace(value["secret_key"])
	if accessKey == "" || secretKey == "" {
		return userSession{}, fmt.Errorf("session missing credentials")
	}

	return userSession{
		AccessKey: accessKey,
		SecretKey: secretKey,
	}, nil
}

func (a *app) requireSession(w http.ResponseWriter, r *http.Request) (userSession, bool) {
	sess, err := a.getSession(r)
	if err != nil {
		a.clearSessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return userSession{}, false
	}
	return sess, true
}

func (a *app) authenticatedS3Client(w http.ResponseWriter, r *http.Request) (*s3.Client, bool) {
	sess, ok := a.requireSession(w, r)
	if !ok {
		return nil, false
	}

	client, err := newS3Client(r.Context(), a.region, a.endpoint, a.forcePathStyle, sess.AccessKey, sess.SecretKey, "")
	if err != nil {
		a.renderError(w, "Could not initialize S3 client", err, http.StatusInternalServerError)
		return nil, false
	}
	return client, true
}

func (a *app) renderLogin(w http.ResponseWriter, accessKey string, loginErr string) {
	a.render(w, "login", map[string]any{
		"Title":           "Login",
		"AccessKey":       accessKey,
		"LoginError":      loginErr,
		"IsAuthenticated": false,
	})
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, err := a.getSession(r); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		a.renderLogin(w, "", "")
		return

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			a.renderLogin(w, "", "invalid form data")
			return
		}

		accessKey := strings.TrimSpace(r.FormValue("access_key"))
		secretKey := strings.TrimSpace(r.FormValue("secret_key"))
		if accessKey == "" || secretKey == "" {
			a.renderLogin(w, accessKey, "access key and secret key are required")
			return
		}

		client, err := newS3Client(r.Context(), a.region, a.endpoint, a.forcePathStyle, accessKey, secretKey, "")
		if err != nil {
			a.renderLogin(w, accessKey, "failed to create S3 client")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, err := client.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
			a.renderLogin(w, accessKey, "login failed: invalid credentials or no bucket access")
			return
		}

		if err := a.setSessionCookie(w, r, userSession{AccessKey: accessKey, SecretKey: secretKey}); err != nil {
			a.renderError(w, "Could not create login session", err, http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	a.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
