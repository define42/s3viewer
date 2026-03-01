package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ---------------- Write operations ----------------

func (a *app) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	bucket := strings.TrimSpace(r.FormValue("bucket"))
	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	in := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	// AWS: for regions != us-east-1, you typically need LocationConstraint
	if a.region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(a.region),
		}
	}

	_, err := a.s3.CreateBucket(ctx, in)
	if err != nil {
		a.renderError(w, "CreateBucket failed", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(bucket)), http.StatusSeeOther)
}

func (a *app) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	// Parse form; file streams from disk/temp.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.renderError(w, "ParseMultipartForm failed", err, http.StatusBadRequest)
		return
	}

	bucket := strings.TrimSpace(r.FormValue("bucket"))
	prefix := r.FormValue("prefix")
	overrideKey := strings.TrimSpace(r.FormValue("key"))

	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	if overrideKey != "" && len(files) > 1 {
		http.Error(w, "key override only supports a single uploaded file", http.StatusBadRequest)
		return
	}

	keyPrefix := strings.TrimPrefix(prefix, "/")
	if keyPrefix != "" && !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}

	ctx := r.Context()
	for _, hdr := range files {
		f, err := hdr.Open()
		if err != nil {
			a.renderError(w, "failed to open uploaded file", err, http.StatusBadRequest)
			return
		}

		key := keyPrefix
		if overrideKey != "" {
			key += strings.TrimPrefix(overrideKey, "/")
		} else {
			key += strings.TrimPrefix(hdr.Filename, "/")
		}
		key = strings.TrimPrefix(key, "/")

		contentType := hdr.Header.Get("Content-Type")

		_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        f,
			ContentType: optionalString(contentType),
		})
		closeErr := f.Close()
		if err != nil {
			a.renderError(w, "PutObject failed", err, http.StatusBadGateway)
			return
		}
		if closeErr != nil {
			a.renderError(w, "failed to close uploaded file", closeErr, http.StatusBadRequest)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(prefix)), http.StatusSeeOther)
}

func (a *app) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	bucket := strings.TrimSpace(r.FormValue("bucket"))
	key := r.FormValue("key")
	if bucket == "" || key == "" {
		http.Error(w, "bucket and key required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err := a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		a.renderError(w, "DeleteObject failed", err, http.StatusBadGateway)
		return
	}

	parent := parentPrefix(key)
	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(parent)), http.StatusSeeOther)
}

func (a *app) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	bucket := strings.TrimSpace(r.FormValue("bucket"))
	if bucket == "" {
		http.Error(w, "bucket required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err := a.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		a.renderError(w, "DeleteBucket failed (bucket must be empty)", err, http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
