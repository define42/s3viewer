package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildAppAndMuxFromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("LISTEN_ADDR", ":9999")
	t.Setenv("AWS_ENDPOINT_URL", "http://localhost:9000")
	t.Setenv("S3_FORCE_PATH_STYLE", "true")
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	a, mux, listen, err := buildAppAndMuxFromEnv()
	if err != nil {
		t.Fatalf("expected build success, got error: %v", err)
	}
	if a == nil {
		t.Fatalf("expected non-nil app")
		return
	}
	if mux == nil {
		t.Fatalf("expected non-nil mux")
		return
	}
	if listen != ":9999" {
		t.Fatalf("unexpected listen address: %q", listen)
	}
	if a.region != "us-east-1" {
		t.Fatalf("unexpected region: %q", a.region)
	}
	if a.endpoint != "http://localhost:9000" {
		t.Fatalf("unexpected endpoint: %q", a.endpoint)
	}
	if !a.forcePathStyle {
		t.Fatalf("expected forcePathStyle true")
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login route to return 200, got %d", loginRec.Code)
	}

	unknownReq := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	unknownRec := httptest.NewRecorder()
	mux.ServeHTTP(unknownRec, unknownReq)
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("expected unknown route to return 404, got %d", unknownRec.Code)
	}
}

func TestBuildAppAndMuxFromEnvEndpointTLSSkip(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	t.Run("skip TLS when S3_ENDPOINT_TLSSKIP=true", func(t *testing.T) {
		t.Setenv("S3_ENDPOINT_TLSSKIP", "true")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if !a.endpointSkipTls {
			t.Fatalf("expected endpointSkipTls=true when S3_ENDPOINT_TLSSKIP=true")
		}
	})

	t.Run("do not skip TLS when S3_ENDPOINT_TLSSKIP=false", func(t *testing.T) {
		t.Setenv("S3_ENDPOINT_TLSSKIP", "false")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if a.endpointSkipTls {
			t.Fatalf("expected endpointSkipTls=false when S3_ENDPOINT_TLSSKIP=false")
		}
	})

	t.Run("do not skip TLS when S3_ENDPOINT_TLSSKIP unset", func(t *testing.T) {
		t.Setenv("S3_ENDPOINT_TLSSKIP", "")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if a.endpointSkipTls {
			t.Fatalf("expected endpointSkipTls=false when S3_ENDPOINT_TLSSKIP unset")
		}
	})
}

func TestBuildAppAndMuxFromEnvUseRgwToken(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	t.Run("enable when USE_RWG_TOKEN=true", func(t *testing.T) {
		t.Setenv("USE_RWG_TOKEN", "true")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if !a.useRgwToken {
			t.Fatalf("expected useRgwToken=true when USE_RWG_TOKEN=true")
		}
	})

	t.Run("enable with case and whitespace", func(t *testing.T) {
		t.Setenv("USE_RWG_TOKEN", " TrUe ")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if !a.useRgwToken {
			t.Fatalf("expected useRgwToken=true for case-insensitive trimmed true")
		}
	})

	t.Run("disable when USE_RWG_TOKEN=false", func(t *testing.T) {
		t.Setenv("USE_RWG_TOKEN", "false")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if a.useRgwToken {
			t.Fatalf("expected useRgwToken=false when USE_RWG_TOKEN=false")
		}
	})

	t.Run("disable when USE_RWG_TOKEN unset", func(t *testing.T) {
		t.Setenv("USE_RWG_TOKEN", "")
		a, _, _, err := buildAppAndMuxFromEnv()
		if err != nil {
			t.Fatalf("expected build success, got error: %v", err)
		}
		if a.useRgwToken {
			t.Fatalf("expected useRgwToken=false when USE_RWG_TOKEN is unset")
		}
	})
}

func TestBuildAppAndMuxFromEnvInvalidSecureCookieConfig(t *testing.T) {
	t.Setenv("SECURECOOKIE_HASH_KEY", "short")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "1234567890123456")

	_, _, _, err := buildAppAndMuxFromEnv()
	if err == nil {
		t.Fatalf("expected error for invalid secure cookie configuration")
	}
}

func TestRunServer(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	original := startHTTPServer
	t.Cleanup(func() { startHTTPServer = original })

	var gotAddr string
	startHTTPServer = func(srv *http.Server) error {
		gotAddr = srv.Addr
		return nil
	}

	if err := runServer(); err != nil {
		t.Fatalf("expected runServer success, got error: %v", err)
	}
	if gotAddr != ":9090" {
		t.Fatalf("expected server addr :9090, got %q", gotAddr)
	}
}

func TestRunServerPropagatesErrors(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("LISTEN_ADDR", ":9091")
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	original := startHTTPServer
	t.Cleanup(func() { startHTTPServer = original })

	startHTTPServer = func(srv *http.Server) error {
		return errors.New("listen failed")
	}

	if err := runServer(); err == nil {
		t.Fatalf("expected runServer to return server start error")
	}
}

func TestMain(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("LISTEN_ADDR", ":9092")
	t.Setenv("SECURECOOKIE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECURECOOKIE_BLOCK_KEY", "abcdef0123456789abcdef0123456789")

	original := startHTTPServer
	t.Cleanup(func() { startHTTPServer = original })

	called := false
	startHTTPServer = func(srv *http.Server) error {
		called = true
		return nil
	}

	main()
	if !called {
		t.Fatalf("expected main to attempt server startup")
	}
}
