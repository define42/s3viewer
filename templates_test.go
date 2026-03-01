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

func TestRenderBucketIncludesPrefixSearch(t *testing.T) {
	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	a.render(rec, "bucket", map[string]any{
		"Title":            "Browse bucket",
		"Bucket":           "my-bucket",
		"Prefix":           "logs/",
		"Search":           "who",
		"BrowseAction":     "/bucket/view/my-bucket",
		"ClearSearchURL":   "/bucket/view/my-bucket?prefix=logs%2F",
		"Crumbs":           []crumb{{Name: "my-bucket", URL: "/bucket/view/my-bucket?prefix="}},
		"BucketTags":       []kv{},
		"BucketTagError":   "",
		"UpPrefix":         "",
		"Folders":          []any{},
		"Objects":          []any{},
		"HasPrev":          false,
		"PrevPageURL":      "",
		"HasNext":          false,
		"NextPageURL":      "",
		"UploadAction":     "/object/upload/my-bucket?prefix=logs%2F",
		"DeleteBucketPOST": "/bucket/delete/my-bucket",
		"IsAuthenticated":  true,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Prefix search:") {
		t.Fatalf("expected prefix search label in bucket template")
	}
	if !strings.Contains(body, `name="search"`) {
		t.Fatalf("expected search input in bucket template")
	}
	if !strings.Contains(body, `type="hidden" name="prefix"`) {
		t.Fatalf("expected hidden prefix input in bucket template")
	}
	if !strings.Contains(body, `action="/bucket/view/my-bucket"`) {
		t.Fatalf("expected prefix search action in bucket template")
	}
}
