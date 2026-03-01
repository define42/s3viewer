package main

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestHandleUploadMultipartReaderFailure(t *testing.T) {
	a := newAuthUnitTestApp()

	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("not-multipart"))
	req = mux.SetURLVars(req, map[string]string{"bucket": "bkt"})
	req.Header.Set("Content-Type", "text/plain")
	addSessionCookieToRequest(t, a, req)

	rec := httptest.NewRecorder()
	a.handleUpload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleUploadValidationErrors(t *testing.T) {
	a := newAuthUnitTestApp()

	t.Run("missing bucket", func(t *testing.T) {
		body, contentType := multipartBody(t, nil, []testUploadFile{{Filename: "a.txt", Contents: "alpha"}})
		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", contentType)
		addSessionCookieToRequest(t, a, req)

		rec := httptest.NewRecorder()
		a.handleUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("no files", func(t *testing.T) {
		body, contentType := multipartBody(t, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req = mux.SetURLVars(req, map[string]string{"bucket": "bkt"})
		req.Header.Set("Content-Type", contentType)
		addSessionCookieToRequest(t, a, req)

		rec := httptest.NewRecorder()
		a.handleUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

}

func TestWriteHandlerFieldValidation(t *testing.T) {
	a := newAuthUnitTestApp()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		path       string
		body       string
		wantStatus int
	}{
		{name: "create bucket missing bucket", handler: a.handleCreateBucket, path: "/bucket/create/", body: "", wantStatus: http.StatusBadRequest},
		{name: "delete object missing key", handler: a.handleDeleteObject, path: "/object/delete", body: "bucket=test", wantStatus: http.StatusBadRequest},
		{name: "delete bucket missing bucket", handler: a.handleDeleteBucket, path: "/bucket/delete", body: "", wantStatus: http.StatusBadRequest},
		{name: "goto bucket missing bucket", handler: a.handleGoToBucket, path: "/bucket/goto", body: "", wantStatus: http.StatusBadRequest},
		{name: "rename object missing new_key", handler: a.handleRenameObject, path: "/object/rename", body: "bucket=test&key=old.txt", wantStatus: http.StatusBadRequest},
		{name: "rename object missing key", handler: a.handleRenameObject, path: "/object/rename", body: "bucket=test&new_key=new.txt", wantStatus: http.StatusBadRequest},
		{name: "rename object same key", handler: a.handleRenameObject, path: "/object/rename", body: "bucket=test&key=same.txt&new_key=same.txt", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			addSessionCookieToRequest(t, a, req)

			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("unexpected status: got=%d want=%d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func multipartBody(t *testing.T, fields map[string]string, files []testUploadFile) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	for _, f := range files {
		part, err := w.CreateFormFile("file", f.Filename)
		if err != nil {
			t.Fatalf("create form file %q: %v", f.Filename, err)
		}
		if _, err := io.Copy(part, bytes.NewBufferString(f.Contents)); err != nil {
			t.Fatalf("write form file %q: %v", f.Filename, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	return &body, w.FormDataContentType()
}

func addSessionCookieToRequest(t *testing.T, a *app, req *http.Request) {
	t.Helper()

	cookieReq := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieRec := httptest.NewRecorder()
	if err := a.setSessionCookie(cookieRec, cookieReq, userSession{AccessKey: "ak", SecretKey: "sk"}); err != nil {
		t.Fatalf("set session cookie: %v", err)
	}
	for _, c := range cookieRec.Result().Cookies() {
		req.AddCookie(c)
	}
}
