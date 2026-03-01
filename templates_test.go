package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewTemplates(t *testing.T) {
	tpl := newTemplates()
	if tpl == nil {
		t.Fatalf("expected non-nil templates")
	}
}

func TestRender(t *testing.T) {
	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	a.render(rec, "login", map[string]any{
		"Title": "Login",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>Login</title>") {
		t.Fatalf("expected login title in rendered body")
	}
	if strings.Contains(body, "Logout") {
		t.Fatalf("did not expect logout button for unauthenticated render")
	}
}

func TestRenderError(t *testing.T) {
	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	a.renderError(rec, "something failed", errors.New("boom"), http.StatusBadGateway)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "something failed") || !strings.Contains(body, "boom") {
		t.Fatalf("expected error content in rendered error body")
	}
}
