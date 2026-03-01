package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gorilla/mux"
)

// ---------------- Write operations ----------------

func (a *app) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	bucket := strings.TrimSpace(r.FormValue("bucket"))
	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	in := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	// AWS: for regions != us-east-1, you typically need LocationConstraint
	if a.region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(a.region),
		}
	}

	_, err := s3Client.CreateBucket(r.Context(), in)
	if err != nil {
		a.renderError(w, "CreateBucket failed", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s?prefix=", url.PathEscape(bucket)), http.StatusSeeOther)
}

func (a *app) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}

	bucket := strings.TrimSpace(mux.Vars(r)["bucket"])
	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	prefix := r.URL.Query().Get("prefix")

	reader, err := r.MultipartReader()
	if err != nil {
		a.renderError(w, "MultipartReader failed", err, http.StatusBadRequest)
		return
	}

	uploadedCount := 0

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.renderError(w, "failed to read multipart data", err, http.StatusBadRequest)
			return
		}

		if part.FormName() != "file" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}

		filename := part.FileName()
		if filename == "" {
			_ = part.Close()
			continue
		}

		var buf bytes.Buffer
		contentType := part.Header.Get("Content-Type")
		_, copyErr := io.Copy(&buf, part)
		closePartErr := part.Close()
		if copyErr != nil {
			a.renderError(w, "failed to read uploaded file", copyErr, http.StatusBadRequest)
			return
		}
		if closePartErr != nil {
			a.renderError(w, "failed to close uploaded file stream", closePartErr, http.StatusBadRequest)
			return
		}

		_, err = s3Client.PutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(prefix + filename),
			Body:        bytes.NewReader(buf.Bytes()),
			ContentType: optionalString(contentType),
		})
		if err != nil {
			a.renderError(w, "Upload failed", err, http.StatusBadGateway)
			return
		}
		uploadedCount++
	}

	if uploadedCount == 0 {
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(prefix)), http.StatusSeeOther)
}

func (a *app) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	vars := mux.Vars(r)
	bucket := strings.TrimSpace(vars["bucket"])
	key := vars["key"]
	if formBucket := strings.TrimSpace(r.FormValue("bucket")); formBucket != "" {
		bucket = formBucket
	}
	if formKey := r.FormValue("key"); formKey != "" {
		key = formKey
	}
	if bucket == "" || key == "" {
		http.Error(w, "bucket and key required", http.StatusBadRequest)
		return
	}

	_, err := s3Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		a.renderError(w, "DeleteObject failed", err, http.StatusBadGateway)
		return
	}

	parent := parentPrefix(key)
	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(parent)), http.StatusSeeOther)
}

func (a *app) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
		return
	}
	bucket := strings.TrimSpace(mux.Vars(r)["bucket"])
	if formBucket := strings.TrimSpace(r.FormValue("bucket")); formBucket != "" {
		bucket = formBucket
	}
	if bucket == "" {
		http.Error(w, "bucket required", http.StatusBadRequest)
		return
	}

	_, err := s3Client.DeleteBucket(r.Context(), &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		a.renderError(w, "DeleteBucket failed (bucket must be empty)", err, http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
