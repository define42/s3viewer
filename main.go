package main

import (
	"crypto/tls"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
)

type app struct {
	tpl             *template.Template
	region          string
	endpoint        string
	forcePathStyle  bool
	cookieName      string
	cookie          *securecookie.SecureCookie
	endpointSkipTls bool
	useRgwToken     bool
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	slog.SetDefault(logger)
	if err := runServer(); err != nil {
		slog.Error("server startup failed", "error", err)
		os.Exit(1)
	}
}

var startHTTPServer = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

var checkEndpoint = func(endpoint string, skipTLS bool) error {
	if endpoint == "" {
		return nil
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if skipTLS {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 -- only when explicitly requested
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := client.Head(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint health check failed for %q: %w", endpoint, err)
	}
	resp.Body.Close()
	return nil
}

func runServer() error {
	a, mux, listen, err := buildAppAndMuxFromEnv()
	if err != nil {
		return fmt.Errorf("startup failed: %w", err)
	}
	time.Sleep(time.Second) // make minio boot

	if err := checkEndpoint(a.endpoint, a.endpointSkipTls); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("s3 viewer listening", "addr", listen, "region", a.region)
	return startHTTPServer(srv)
}

func buildAppAndMuxFromEnv() (*app, http.Handler, string, error) {
	region := getenvAny("eu-west-1", "AWS_REGION", "S3_REGION")
	listen := getenv("LISTEN_ADDR", ":8080")
	endpoint := getenvAny("", "AWS_ENDPOINT_URL", "S3_ENDPOINT")
	endpointSkipTls := strings.EqualFold(strings.TrimSpace(getenv("S3_ENDPOINT_TLSSKIP", "")), "true")
	forcePathStyle := strings.EqualFold(strings.TrimSpace(getenv("S3_FORCE_PATH_STYLE", "")), "true")
	useRgwToken := strings.EqualFold(strings.TrimSpace(getenv("USE_RWG_TOKEN", "")), "true")

	sc, err := newSecureCookieFromEnv()
	if err != nil {
		return nil, nil, "", fmt.Errorf("securecookie config: %w", err)
	}

	a := &app{
		tpl:             newTemplates(),
		region:          region,
		endpoint:        endpoint,
		forcePathStyle:  forcePathStyle,
		cookieName:      sessionCookieName,
		cookie:          sc,
		endpointSkipTls: endpointSkipTls,
		useRgwToken:     useRgwToken,
	}

	return a, newAppMux(a), listen, nil
}

func newAppMux(a *app) http.Handler {
	router := mux.NewRouter()
	router.UseEncodedPath()
	router.HandleFunc("/login", a.handleLogin)
	router.HandleFunc("/logout", a.handleLogout)

	// WRITE (POST)
	router.HandleFunc("/bucket/goto", a.handleGoToBucket)
	router.HandleFunc("/bucket/create/{bucket}", a.handleCreateBucket)
	router.HandleFunc("/bucket/delete/{bucket}", a.handleDeleteBucket)
	router.HandleFunc("/object/upload/{bucket}", a.handleUpload)
	router.HandleFunc("/object/delete/{bucket}/{key:.+}", a.handleDeleteObject)
	router.HandleFunc("/object/rename/{bucket}/{key:.+}", a.handleRenameObject)

	// READ
	router.HandleFunc("/", a.handleIndex)                                                 // list buckets + forms
	router.HandleFunc("/bucket/view/{bucket}", a.handleBucketBrowse)                      // browse bucket root
	router.PathPrefix("/object/view/{bucket}/{key:.+}").HandlerFunc(a.handleObject)       // object details (tags + metadata)
	router.PathPrefix("/object/download/{bucket}/{key:.+}").HandlerFunc(a.handleDownload) // download
	return router
}
