package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// aws_config.go: loadAWSConfigWithStaticCredentials – TLS skip path
// ---------------------------------------------------------------------------

func TestLoadAWSConfigWithSkipTLS(t *testing.T) {
	cfg, err := loadAWSConfigWithStaticCredentials(
		context.Background(), "us-east-1", "https://localhost:9000",
		"test-access-key", "test-secret-key", "", true, false,
	)
	if err != nil {
		t.Fatalf("expected success with endpointSkipTls=true, got: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
}

// ---------------------------------------------------------------------------
// templates.go: render – error path (unknown template name)
// ---------------------------------------------------------------------------

func TestRenderUnknownTemplate(t *testing.T) {
	a := newAuthUnitTestApp()
	rec := httptest.NewRecorder()
	// ExecuteTemplate returns an error for an undefined template name.
	// render writes the Content-Type header but ExecuteTemplate writes nothing
	// before failing, so http.Error(w, "template error", 500) sets the status.
	a.render(rec, "does-not-exist", map[string]any{"Title": "X"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "template error") {
		t.Fatalf("expected 'template error' in body, got: %q", body)
	}
}

// ---------------------------------------------------------------------------
// handlers_buckets.go: handleGoToBucket – HeadBucket S3 error
// ---------------------------------------------------------------------------

func TestHandleGoToBucketS3Error(t *testing.T) {
	a := newAuthUnitTestApp() // endpoint: http://localhost:9000 (nothing running)

	body := "bucket=test-bucket"
	req := httptest.NewRequest(http.MethodPost, "/bucket/goto", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleGoToBucket(rec, req)

	// HeadBucket fails because nothing listens on localhost:9000 → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_buckets.go: handleBucketBrowse – S3 errors (GetBucketTagging +
// ListObjectsV2 both fail because the endpoint is unreachable)
// ---------------------------------------------------------------------------

func TestHandleBucketBrowseS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/bucket/view/test-bucket", nil)
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleBucketBrowse(rec, req)

	// GetBucketTagging fails (non-fatal, sets bucketTagError).
	// ListObjectsV2 then fails → renderError → 502.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_objects.go: handleObject – HeadObject S3 error
// ---------------------------------------------------------------------------

func TestHandleObjectS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/view/test-bucket/mykey.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "test-bucket", "key": "mykey.txt"})
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleObject(rec, req)

	// HeadObject fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// handleObject: url.PathUnescape failure for an invalid bucket escape sequence.
// The mux var contains %GG (invalid percent-encoding); the request URL itself
// must be valid so httptest.NewRequest does not panic.
func TestHandleObjectInvalidBucketEscape(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/view/bucket/key.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "%GG", "key": "key.txt"})

	rec := httptest.NewRecorder()
	a.handleObject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid bucket escape, got %d", rec.Code)
	}
}

// handleObject: url.PathUnescape failure for an invalid key escape sequence.
func TestHandleObjectInvalidKeyEscape(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/view/bucket/key.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "bucket", "key": "%GG"})

	rec := httptest.NewRecorder()
	a.handleObject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid key escape, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_objects.go: handleDownload – GetObject S3 error
// ---------------------------------------------------------------------------

func TestHandleDownloadS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/download/test-bucket/mykey.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "test-bucket", "key": "mykey.txt"})
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleDownload(rec, req)

	// GetObject fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// handleDownload: invalid escape sequences in bucket/key
func TestHandleDownloadInvalidBucketEscape(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/download/bucket/key.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "%GG", "key": "key.txt"})

	rec := httptest.NewRecorder()
	a.handleDownload(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid bucket escape, got %d", rec.Code)
	}
}

func TestHandleDownloadInvalidKeyEscape(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodGet, "/object/download/bucket/key.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "bucket", "key": "%GG"})

	rec := httptest.NewRecorder()
	a.handleDownload(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid key escape, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_write.go: handleRenameObject – CopyObject S3 error
// ---------------------------------------------------------------------------

func TestHandleRenameObjectS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	body := "bucket=test-bucket&key=old.txt&new_key=new.txt"
	req := httptest.NewRequest(http.MethodPost, "/object/rename/test-bucket/old.txt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleRenameObject(rec, req)

	// CopyObject fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_write.go: handleUpload – non-file part skipped, then PutObject error
// ---------------------------------------------------------------------------

func TestHandleUploadNonFilePartAndS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	// Include a non-"file" field (should be skipped) AND a real file.
	body, contentType := multipartBody(t,
		map[string]string{"extra_field": "value"},
		[]testUploadFile{{Filename: "hello.txt", Contents: "hello"}},
	)
	req := httptest.NewRequest(http.MethodPost, "/object/upload/test-bucket", body)
	req = mux.SetURLVars(req, map[string]string{"bucket": "test-bucket"})
	req.Header.Set("Content-Type", contentType)
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleUpload(rec, req)

	// PutObject fails because nothing listens on localhost:9000 → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_write.go: handleCreateBucket – S3 error
// ---------------------------------------------------------------------------

func TestHandleCreateBucketS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodPost, "/bucket/create/new-bucket", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "new-bucket"})
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleCreateBucket(rec, req)

	// CreateBucket fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_write.go: handleDeleteObject – DeleteObject S3 error
// ---------------------------------------------------------------------------

func TestHandleDeleteObjectS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	body := "bucket=test-bucket&key=some/object.txt"
	req := httptest.NewRequest(http.MethodPost, "/object/delete/test-bucket/some/object.txt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleDeleteObject(rec, req)

	// DeleteObject fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handlers_write.go: handleDeleteBucket – DeleteBucket S3 error
// ---------------------------------------------------------------------------

func TestHandleDeleteBucketS3Error(t *testing.T) {
	a := newAuthUnitTestApp()

	body := "bucket=old-bucket"
	req := httptest.NewRequest(http.MethodPost, "/bucket/delete/old-bucket", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleDeleteBucket(rec, req)

	// DeleteBucket fails → renderError → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}
