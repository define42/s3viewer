package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestLoadAWSConfigWithStaticCredentialsRequiresKeys(t *testing.T) {
	_, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "", "", "", "", false, false)
	if err == nil {
		t.Fatalf("expected error when credentials are missing")
	}
}

func TestLoadAWSConfigWithStaticCredentialsInvalidEndpoint(t *testing.T) {
	_, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "::://bad-endpoint", "ak", "sk", "", false, false)
	if err == nil {
		t.Fatalf("expected error for invalid endpoint")
	}
}

func TestLoadAWSConfigWithStaticCredentialsSuccess(t *testing.T) {
	cfg, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "http://localhost:9000", "ak", "sk", "", false, false)
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

func TestLoadAWSConfigWithStaticCredentialsUseRgwToken(t *testing.T) {
	expectedToken, err := generateRGWToken("ak", "sk")
	if err != nil {
		t.Fatalf("expected generateRGWToken success, got error: %v", err)
	}

	cfg, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "http://localhost:9000", "ak", "sk", "", false, true)
	if err != nil {
		t.Fatalf("expected config load success with useRgwToken=true, got error: %v", err)
	}

	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("expected credentials retrieval success, got error: %v", err)
	}
	if creds.AccessKeyID != expectedToken {
		t.Fatalf("expected access key to be RGW token, got %q", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "sk" {
		t.Fatalf("unexpected secret access key: %q", creds.SecretAccessKey)
	}
}

func TestNewS3ClientRequiresKeys(t *testing.T) {
	_, err := newS3Client(context.Background(), "us-east-1", "", true, "", "", "", false, false)
	if err == nil {
		t.Fatalf("expected error when creating client without credentials")
	}
}

func TestAssumeRole(t *testing.T) {
	var requestValues url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		requestValues, err = url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("parse request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>assumed-ak</AccessKeyId><SecretAccessKey>assumed-sk</SecretAccessKey><SessionToken>token</SessionToken><Expiration>2030-01-01T00:00:00Z</Expiration></Credentials></AssumeRoleResult><ResponseMetadata><RequestId>request-id</RequestId></ResponseMetadata></AssumeRoleResponse>`)
	}))
	defer server.Close()

	creds, err := assumeRole(context.Background(), "us-east-1", server.URL, "ak", "sk", "arn:aws:iam::123456789012:role/viewer", "viewer-session", "external", false)
	if err != nil {
		t.Fatalf("assumeRole returned error: %v", err)
	}
	if creds.AccessKeyID != "assumed-ak" || creds.SecretAccessKey != "assumed-sk" || creds.SessionToken != "token" {
		t.Fatalf("unexpected credentials: %#v", creds)
	}
	if !creds.CanExpire || creds.Expires.IsZero() {
		t.Fatalf("expected expiring credentials: %#v", creds)
	}
	if requestValues.Get("RoleArn") != "arn:aws:iam::123456789012:role/viewer" {
		t.Fatalf("unexpected RoleArn: %q", requestValues.Get("RoleArn"))
	}
	if requestValues.Get("RoleSessionName") != "viewer-session" {
		t.Fatalf("unexpected RoleSessionName: %q", requestValues.Get("RoleSessionName"))
	}
	if requestValues.Get("ExternalId") != "external" {
		t.Fatalf("unexpected ExternalId: %q", requestValues.Get("ExternalId"))
	}
}

func TestLoadAWSConfigWithS3Debug(t *testing.T) {
	t.Setenv("S3_DEBUG", "true")
	cfg, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "", "ak", "sk", "", false, false)
	if err != nil {
		t.Fatalf("expected config load success, got error: %v", err)
	}
	if cfg.Logger == nil {
		t.Fatal("expected Logger to be set when S3_DEBUG=true")
	}
	expectedMode := aws.LogRetries | aws.LogRequest | aws.LogResponse
	if cfg.ClientLogMode != expectedMode {
		t.Fatalf("expected ClientLogMode %v, got %v", expectedMode, cfg.ClientLogMode)
	}
}

func TestLoadAWSConfigWithoutS3Debug(t *testing.T) {
	t.Setenv("S3_DEBUG", "")
	cfg, err := loadAWSConfigWithStaticCredentials(context.Background(), "us-east-1", "", "ak", "sk", "", false, false)
	if err != nil {
		t.Fatalf("expected config load success, got error: %v", err)
	}
	if cfg.ClientLogMode != 0 {
		t.Fatalf("expected ClientLogMode to be 0, got %v", cfg.ClientLogMode)
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
