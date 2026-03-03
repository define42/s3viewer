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

func TestRenderIndexIncludesCreateBucketPathAction(t *testing.T) {
	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	a.render(rec, "index", map[string]any{
		"Title":           "Buckets",
		"Buckets":         []any{},
		"IsAuthenticated": true,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/bucket/create/"`) {
		t.Fatalf("expected create bucket form action to include /bucket/create/")
	}
	if !strings.Contains(body, `onsubmit="return setCreateBucketAction(this);"`) {
		t.Fatalf("expected create bucket form to set dynamic path action on submit")
	}
	if !strings.Contains(body, `form.action = "/bucket/create/" + encodeURIComponent(bucket);`) {
		t.Fatalf("expected create bucket form script to URL-encode bucket path segment")
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
		"LifecycleRules":   []any{},
		"LifecycleError":   "",
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

func TestRenderBucketPolicy(t *testing.T) {
	a := newAuthUnitTestApp()

	// Test with a policy present
	rec := httptest.NewRecorder()
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::my-bucket/*"}]}`
	a.render(rec, "bucket", map[string]any{
		"Title":             "Browse bucket",
		"Bucket":            "my-bucket",
		"Prefix":            "",
		"Search":            "",
		"BrowseAction":      "/bucket/view/my-bucket",
		"ClearSearchURL":    "/bucket/view/my-bucket?prefix=",
		"Crumbs":            []crumb{{Name: "my-bucket", URL: "/bucket/view/my-bucket?prefix="}},
		"BucketTags":        []kv{},
		"BucketTagError":    "",
		"UpPrefix":          "",
		"Folders":           []any{},
		"Objects":           []any{},
		"HasPrev":           false,
		"PrevPageURL":       "",
		"HasNext":           false,
		"NextPageURL":       "",
		"UploadAction":      "/object/upload/my-bucket?prefix=",
		"DeleteBucketPOST":  "/bucket/delete/my-bucket",
		"IsAuthenticated":   true,
		"LifecycleRules":    []any{},
		"LifecycleError":    "",
		"BucketPolicy":      policy,
		"BucketPolicyError": "",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Bucket policy") {
		t.Fatalf("expected bucket policy section in bucket template")
	}
	if !strings.Contains(body, "s3:GetObject") {
		t.Fatalf("expected policy content in bucket template")
	}

	// Test with no policy
	rec2 := httptest.NewRecorder()
	a.render(rec2, "bucket", map[string]any{
		"Title":             "Browse bucket",
		"Bucket":            "my-bucket",
		"Prefix":            "",
		"Search":            "",
		"BrowseAction":      "/bucket/view/my-bucket",
		"ClearSearchURL":    "/bucket/view/my-bucket?prefix=",
		"Crumbs":            []crumb{{Name: "my-bucket", URL: "/bucket/view/my-bucket?prefix="}},
		"BucketTags":        []kv{},
		"BucketTagError":    "",
		"UpPrefix":          "",
		"Folders":           []any{},
		"Objects":           []any{},
		"HasPrev":           false,
		"PrevPageURL":       "",
		"HasNext":           false,
		"NextPageURL":       "",
		"UploadAction":      "/object/upload/my-bucket?prefix=",
		"DeleteBucketPOST":  "/bucket/delete/my-bucket",
		"IsAuthenticated":   true,
		"LifecycleRules":    []any{},
		"LifecycleError":    "",
		"BucketPolicy":      "",
		"BucketPolicyError": "",
	})

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, "No bucket policy.") {
		t.Fatalf("expected 'No bucket policy.' when policy is empty")
	}

	// Test with policy error
	rec3 := httptest.NewRecorder()
	a.render(rec3, "bucket", map[string]any{
		"Title":             "Browse bucket",
		"Bucket":            "my-bucket",
		"Prefix":            "",
		"Search":            "",
		"BrowseAction":      "/bucket/view/my-bucket",
		"ClearSearchURL":    "/bucket/view/my-bucket?prefix=",
		"Crumbs":            []crumb{{Name: "my-bucket", URL: "/bucket/view/my-bucket?prefix="}},
		"BucketTags":        []kv{},
		"BucketTagError":    "",
		"UpPrefix":          "",
		"Folders":           []any{},
		"Objects":           []any{},
		"HasPrev":           false,
		"PrevPageURL":       "",
		"HasNext":           false,
		"NextPageURL":       "",
		"UploadAction":      "/object/upload/my-bucket?prefix=",
		"DeleteBucketPOST":  "/bucket/delete/my-bucket",
		"IsAuthenticated":   true,
		"LifecycleRules":    []any{},
		"LifecycleError":    "",
		"BucketPolicy":      "",
		"BucketPolicyError": "unavailable",
	})

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec3.Code)
	}
	body3 := rec3.Body.String()
	if !strings.Contains(body3, "unavailable") {
		t.Fatalf("expected error message when policy is unavailable")
	}
}

func TestRenderBucketLifecycleConfiguration(t *testing.T) {
	type lifecycleRuleRow struct {
		ID          string
		Status      string
		Prefix      string
		Expiration  string
		Transitions []string
		AbortDays   string
	}

	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	a.render(rec, "bucket", map[string]any{
		"Title":            "Browse bucket",
		"Bucket":           "my-bucket",
		"Prefix":           "",
		"Search":           "",
		"BrowseAction":     "/bucket/view/my-bucket",
		"ClearSearchURL":   "/bucket/view/my-bucket?prefix=",
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
		"UploadAction":     "/object/upload/my-bucket?prefix=",
		"DeleteBucketPOST": "/bucket/delete/my-bucket",
		"IsAuthenticated":  true,
		"LifecycleRules": []lifecycleRuleRow{
			{
				ID:          "expire-old",
				Status:      "Enabled",
				Prefix:      "logs/",
				Expiration:  "90 days",
				Transitions: []string{"GLACIER after 30 days"},
				AbortDays:   "7 days",
			},
		},
		"LifecycleError": "",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Lifecycle configuration") {
		t.Fatalf("expected lifecycle configuration section in bucket template")
	}
	if !strings.Contains(body, "expire-old") {
		t.Fatalf("expected lifecycle rule ID in bucket template")
	}
	if !strings.Contains(body, "90 days") {
		t.Fatalf("expected expiration in lifecycle rule row")
	}
	if !strings.Contains(body, "GLACIER after 30 days") {
		t.Fatalf("expected transition in lifecycle rule row")
	}
	if !strings.Contains(body, "7 days") {
		t.Fatalf("expected abort days in lifecycle rule row")
	}
}
