package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogRequests(t *testing.T) {
	called := false
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestKeepPrintable(t *testing.T) {
	tests := []struct {
		in   rune
		want rune
	}{
		{'\n', '_'},
		{'\r', '_'},
		{0x00, '_'},
		{0x1f, '_'},
		{0x7f, '_'},
		{0x80, '_'},
		{0x9f, '_'},
		{0x2028, '_'},
		{0x2029, '_'},
		{'A', 'A'},
		{'/', '/'},
		{' ', ' '},
		{0xa0, 0xa0}, // non-breaking space: above C1 range, not filtered
	}
	for _, tc := range tests {
		if got := keepPrintable(tc.in); got != tc.want {
			t.Errorf("keepPrintable(%U) = %U, want %U", tc.in, got, tc.want)
		}
	}
}
