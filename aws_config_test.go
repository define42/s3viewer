package main

import (
	"context"
	"testing"
)

func TestLoadAWSConfigWithStaticCredentialsRequiresKeys(t *testing.T) {
	_, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "", "", "", "", false)
	if err == nil {
		t.Fatalf("expected error when credentials are missing")
	}
}

func TestLoadAWSConfigWithStaticCredentialsInvalidEndpoint(t *testing.T) {
	_, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "::://bad-endpoint", "ak", "sk", "", false)
	if err == nil {
		t.Fatalf("expected error for invalid endpoint")
	}
}

func TestLoadAWSConfigWithStaticCredentialsSuccess(t *testing.T) {
	cfg, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "http://localhost:9000", "ak", "sk", "", false)
	if err != nil {
		t.Fatalf("expected config load success, got error: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("unexpected region: %q", cfg.Region)
	}
	if cfg.BaseEndpoint == nil || *cfg.BaseEndpoint != "http://localhost:9000" {
		t.Fatalf("unexpected base endpoint: %#v", cfg.BaseEndpoint)
	}
}

func TestNewS3ClientRequiresKeys(t *testing.T) {
	_, err := newS3Client(context.Background(), "us-east-1", "", true, "", "", "", false)
	if err == nil {
		t.Fatalf("expected error when creating client without credentials")
	}
}

func TestGetenvAndGetenvAny(t *testing.T) {
	t.Setenv("FIRST_ENV", "")
	t.Setenv("SECOND_ENV", "value2")
	if got := getenv("MISSING_ENV", "default"); got != "default" {
		t.Fatalf("expected default from getenv, got %q", got)
	}
	if got := getenvAny("fallback", "FIRST_ENV", "SECOND_ENV"); got != "value2" {
		t.Fatalf("expected second env value, got %q", got)
	}
	if got := getenvAny("fallback", "FIRST_ENV", "NOT_SET"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}
}
