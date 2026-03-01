package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(bucket)), http.StatusSeeOther)
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

	reader, err := r.MultipartReader()
	if err != nil {
		a.renderError(w, "MultipartReader failed", err, http.StatusBadRequest)
		return
	}

	type uploadPart struct {
		filename    string
		contentType string
		tmpPath     string
	}
	tempDir := os.TempDir()
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		tempDir = "."
	}
	uploads := []uploadPart{}
	defer func() {
		for _, u := range uploads {
			_ = os.Remove(u.tmpPath)
		}
	}()

	bucket := ""
	prefix := ""
	overrideKey := ""

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.renderError(w, "failed to read multipart data", err, http.StatusBadRequest)
			return
		}

		switch part.FormName() {
		case "bucket":
			val, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				a.renderError(w, "failed to read bucket field", readErr, http.StatusBadRequest)
				return
			}
			bucket = strings.TrimSpace(string(val))

		case "prefix":
			val, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				a.renderError(w, "failed to read prefix field", readErr, http.StatusBadRequest)
				return
			}
			prefix = string(val)

		case "key":
			val, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				a.renderError(w, "failed to read key field", readErr, http.StatusBadRequest)
				return
			}
			overrideKey = strings.TrimSpace(string(val))

		case "file":
			filename := part.FileName()
			if filename == "" {
				_ = part.Close()
				continue
			}

			tmpFile, tmpErr := os.CreateTemp(tempDir, "s3viewer-upload-*")
			if tmpErr != nil {
				_ = part.Close()
				a.renderError(w, "failed to create temp file", tmpErr, http.StatusInternalServerError)
				return
			}
			tmpPath := tmpFile.Name()
			contentType := part.Header.Get("Content-Type")

			_, copyErr := io.Copy(tmpFile, part)
			closePartErr := part.Close()
			closeTmpErr := tmpFile.Close()
			if copyErr != nil {
				_ = os.Remove(tmpPath)
				a.renderError(w, "failed to read uploaded file", copyErr, http.StatusBadRequest)
				return
			}
			if closePartErr != nil {
				_ = os.Remove(tmpPath)
				a.renderError(w, "failed to finalize uploaded file stream", closePartErr, http.StatusBadRequest)
				return
			}
			if closeTmpErr != nil {
				_ = os.Remove(tmpPath)
				a.renderError(w, "failed to finalize temp file", closeTmpErr, http.StatusInternalServerError)
				return
			}

			uploads = append(uploads, uploadPart{
				filename:    filename,
				contentType: contentType,
				tmpPath:     tmpPath,
			})

		default:
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
		}
	}

	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	if len(uploads) == 0 {
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	if overrideKey != "" && len(uploads) > 1 {
		http.Error(w, "key override only supports a single uploaded file", http.StatusBadRequest)
		return
	}

	keyPrefix := strings.TrimPrefix(prefix, "/")
	if keyPrefix != "" && !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}

	for _, upload := range uploads {
		f, err := os.Open(upload.tmpPath)
		if err != nil {
			a.renderError(w, "failed to open temp upload file", err, http.StatusInternalServerError)
			return
		}

		key := keyPrefix
		if overrideKey != "" {
			key += strings.TrimPrefix(overrideKey, "/")
		} else {
			key += strings.TrimPrefix(upload.filename, "/")
		}
		key = strings.TrimPrefix(key, "/")

		_, err = s3Client.PutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        f,
			ContentType: optionalString(upload.contentType),
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
	key := r.FormValue("key")
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
	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(parent)), http.StatusSeeOther)
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
	bucket := strings.TrimSpace(r.FormValue("bucket"))
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
