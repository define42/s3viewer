package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
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

func buildAppAndMuxFromEnv() (*app, http.Handler, string, error) {
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

func newAppMux(a *app) http.Handler {
	router := mux.NewRouter()
	router.UseEncodedPath()
	router.HandleFunc("/login", a.handleLogin)
	router.HandleFunc("/logout", a.handleLogout)

	// WRITE (POST)
	router.HandleFunc("/bucket/goto", a.handleGoToBucket)
	router.HandleFunc("/bucket/create", a.handleCreateBucket)
	router.HandleFunc("/bucket/delete", a.handleDeleteBucket)
	router.HandleFunc("/object/upload/{bucket}", a.handleUpload)
	router.HandleFunc("/object/delete/{bucket}/{key}", a.handleDeleteObject)

	// READ
	router.HandleFunc("/", a.handleIndex)                           // list buckets + forms
	router.PathPrefix("/bucket/").HandlerFunc(a.handleBucketBrowse) // browse bucket
	router.PathPrefix("/object/").HandlerFunc(a.handleObject)       // object details (tags + metadata)
	router.PathPrefix("/download/").HandlerFunc(a.handleDownload)   // download
	return router
}
