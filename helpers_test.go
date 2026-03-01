package main

import (
	"strings"
	"testing"
	"time"
)

func TestOptionalString(t *testing.T) {
	if got := optionalString("   "); got != nil {
		t.Fatalf("expected nil for blank string")
	}
	got := optionalString("value")
	if got == nil || *got != "value" {
		t.Fatalf("expected pointer to \"value\"")
	}
}

func TestParseIntClamp(t *testing.T) {
	if got := parseIntClamp("", 10, 1, 100); got != 10 {
		t.Fatalf("expected default 10, got %d", got)
	}
	if got := parseIntClamp("not-an-int", 10, 1, 100); got != 10 {
		t.Fatalf("expected default 10 for invalid int, got %d", got)
	}
	if got := parseIntClamp("0", 10, 1, 100); got != 1 {
		t.Fatalf("expected min clamp 1, got %d", got)
	}
	if got := parseIntClamp("200", 10, 1, 100); got != 100 {
		t.Fatalf("expected max clamp 100, got %d", got)
	}
	if got := parseIntClamp("42", 10, 1, 100); got != 42 {
		t.Fatalf("expected parsed 42, got %d", got)
	}
}

func TestTimeStr(t *testing.T) {
	if got := timeStr(nil); got != "" {
		t.Fatalf("expected empty string for nil time")
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := timeStr(&ts); got != "2026-01-02T03:04:05Z" {
		t.Fatalf("unexpected RFC3339 output: %s", got)
	}
}

func TestHumanBytes(t *testing.T) {
	if got := humanBytes(999); got != "999 B" {
		t.Fatalf("unexpected bytes formatting: %s", got)
	}
	if got := humanBytes(1024); got != "1.00 KiB" {
		t.Fatalf("unexpected kib formatting: %s", got)
	}
	if got := humanBytes(5 * 1024 * 1024); got != "5.00 MiB" {
		t.Fatalf("unexpected mib formatting: %s", got)
	}
}

func TestParentPrefix(t *testing.T) {
	if got := parentPrefix(""); got != "" {
		t.Fatalf("expected empty parent for empty prefix")
	}
	if got := parentPrefix("file.txt"); got != "" {
		t.Fatalf("expected empty parent for top-level file")
	}
	if got := parentPrefix("a/b/c.txt"); got != "a/b/" {
		t.Fatalf("expected a/b/, got %s", got)
	}
	if got := parentPrefix("a/b/c/"); got != "a/b/" {
		t.Fatalf("expected a/b/, got %s", got)
	}
}

func TestBreadcrumbs(t *testing.T) {
	crumbs := breadcrumbs("my-bucket", "a/b/")
	if len(crumbs) != 3 {
		t.Fatalf("expected 3 breadcrumbs, got %d", len(crumbs))
	}
	if crumbs[0].Name != "my-bucket" || crumbs[1].Name != "a" || crumbs[2].Name != "b" {
		t.Fatalf("unexpected breadcrumb names: %#v", crumbs)
	}
}

func TestBucketBrowseURL(t *testing.T) {
	url := bucketBrowseURL("my-bucket", "a/", "tok123", []string{"p1", "p2"})
	if url == "" {
		t.Fatalf("expected non-empty URL")
	}
	if want := "/bucket/my-bucket?"; len(url) <= len(want) || url[:len(want)] != want {
		t.Fatalf("unexpected browse url prefix: %s", url)
	}
	if want := "token=tok123"; !strings.Contains(url, want) {
		t.Fatalf("expected %q in url %q", want, url)
	}
	if want := "prev=p1"; !strings.Contains(url, want) {
		t.Fatalf("expected %q in url %q", want, url)
	}
	if want := "prev=p2"; !strings.Contains(url, want) {
		t.Fatalf("expected %q in url %q", want, url)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("\"bad\nname\r"); got != "_bad_name_" {
		t.Fatalf("unexpected sanitized filename: %q", got)
	}
	if got := sanitizeFilename(""); got != "download" {
		t.Fatalf("expected fallback filename, got %q", got)
	}
}
