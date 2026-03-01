package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type app struct {
	s3     *s3.Client
	tpl    *template.Template
	region string
}

func main() {
	ctx := context.Background()

	region := getenvAny("eu-west-1", "AWS_REGION", "S3_REGION")
	listen := getenv("LISTEN_ADDR", ":8080")
	forcePathStyle := strings.EqualFold(strings.TrimSpace(os.Getenv("S3_FORCE_PATH_STYLE")), "true")

	awsCfg, err := loadAWSConfig(ctx, region)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = forcePathStyle
	})

	a := &app{
		s3:     client,
		tpl:    newTemplates(),
		region: region,
	}

	mux := http.NewServeMux()

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

	srv := &http.Server{
		Addr:              listen,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("S3 Viewer running on http://localhost%s (region=%s)", listen, region)
	log.Fatal(srv.ListenAndServe())
}
