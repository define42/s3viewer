package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerPathGuards(t *testing.T) {
	a := newAuthUnitTestApp()

	tests := []struct {
		name       string
		path       string
		handler    http.HandlerFunc
		wantStatus int
	}{
		{name: "index not found", path: "/nope", handler: a.handleIndex, wantStatus: http.StatusNotFound},
		{name: "bucket browse invalid", path: "/bucket/", handler: a.handleBucketBrowse, wantStatus: http.StatusNotFound},
		{name: "object invalid", path: "/object/view/only-bucket", handler: a.handleObject, wantStatus: http.StatusNotFound},
		{name: "download invalid", path: "/object/download/only-bucket", handler: a.handleDownload, wantStatus: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("unexpected status: got=%d want=%d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestWriteHandlersMethodGuards(t *testing.T) {
	a := newAuthUnitTestApp()

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "create bucket", handler: a.handleCreateBucket},
		{name: "upload", handler: a.handleUpload},
		{name: "delete object", handler: a.handleDeleteObject},
		{name: "delete bucket", handler: a.handleDeleteBucket},
		{name: "delete lifecycle", handler: a.handleDeleteLifecycle},
		{name: "go to bucket", handler: a.handleGoToBucket},
		{name: "rename object", handler: a.handleRenameObject},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405, got %d", rec.Code)
			}
		})
	}
}

func TestHandlersRequireSessionRedirect(t *testing.T) {
	a := newAuthUnitTestApp()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{name: "index", handler: a.handleIndex, method: http.MethodGet, path: "/"},
		{name: "create bucket", handler: a.handleCreateBucket, method: http.MethodPost, path: "/bucket/create/test-bucket"},
		{name: "upload", handler: a.handleUpload, method: http.MethodPost, path: "/object/upload/test-bucket"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("expected 303 redirect, got %d", rec.Code)
			}
			if got := rec.Header().Get("Location"); got != "/login" {
				t.Fatalf("expected redirect to /login, got %q", got)
			}
		})
	}
}
