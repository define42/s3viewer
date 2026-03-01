package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
)

type app struct {
	tpl            *template.Template
	region         string
	endpoint       string
	forcePathStyle bool
	cookieName     string
	cookie         *securecookie.SecureCookie
}

func main() {
	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

var startHTTPServer = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func runServer() error {
	a, mux, listen, err := buildAppAndMuxFromEnv()
	if err != nil {
		return fmt.Errorf("startup failed: %w", err)
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("S3 Viewer running on http://localhost%s (region=%s)", listen, a.region)
	return startHTTPServer(srv)
}

func buildAppAndMuxFromEnv() (*app, *http.ServeMux, string, error) {
	region := getenvAny("eu-west-1", "AWS_REGION", "S3_REGION")
	listen := getenv("LISTEN_ADDR", ":8080")
	endpoint := getenvAny("", "AWS_ENDPOINT_URL", "S3_ENDPOINT")
	forcePathStyle := strings.EqualFold(strings.TrimSpace(getenv("S3_FORCE_PATH_STYLE", "")), "true")

	sc, err := newSecureCookieFromEnv()
	if err != nil {
		return nil, nil, "", fmt.Errorf("securecookie config: %w", err)
	}

	a := &app{
		tpl:            newTemplates(),
		region:         region,
		endpoint:       endpoint,
		forcePathStyle: forcePathStyle,
		cookieName:     sessionCookieName,
		cookie:         sc,
	}

	return a, newAppMux(a), listen, nil
}

func newAppMux(a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", a.handleLogin)
	mux.HandleFunc("/logout", a.handleLogout)

	// READ
	mux.HandleFunc("/", a.handleIndex)               // list buckets + forms
	mux.HandleFunc("/bucket/", a.handleBucketBrowse) // browse bucket
	mux.HandleFunc("/object/", a.handleObject)       // object details (tags + metadata)
	mux.HandleFunc("/download/", a.handleDownload)   // download

	// WRITE (POST)
	mux.HandleFunc("/bucket/goto", a.handleGoToBucket)
	mux.HandleFunc("/bucket/create", a.handleCreateBucket)
	mux.HandleFunc("/bucket/delete", a.handleDeleteBucket)
	mux.HandleFunc("/upload", a.handleUpload)
	mux.HandleFunc("/object/delete", a.handleDeleteObject)
	return mux
}
