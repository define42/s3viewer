package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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
	bucket := strings.TrimSpace(mux.Vars(r)["bucket"])
	if bucket == "" {
		http.Error(w, "bucket is required", http.StatusBadRequest)
		return
	}

	in := &s3.CreateBucketInput{Bucket: aws.String(bucket)}

	_, err := s3Client.CreateBucket(r.Context(), in)
	if err != nil {
		a.renderError(w, "CreateBucket failed", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
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

	reader, err := r.MultipartReader()
	if err != nil {
		a.renderError(w, "MultipartReader failed", err, http.StatusBadRequest)
		return
	}

	const (
		maxPartSize = 64 << 20 // 64 MiB
	)

	uploadedCount := 0
	var uploadFileNames []string

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
		uploadFileNames = append(uploadFileNames, filename)

		contentType := part.Header.Get("Content-Type")

		// Read up to 64MiB+1 into memory to decide strategy.
		head, readErr := io.ReadAll(io.LimitReader(part, maxPartSize+1))
		if readErr != nil {
			_ = part.Close()
			a.renderError(w, "failed to read uploaded file", readErr, http.StatusBadRequest)
			return
		}

		// SMALL: PutObject
		if int64(len(head)) <= maxPartSize {
			_, putErr := s3Client.PutObject(r.Context(), &s3.PutObjectInput{
				Bucket:      aws.String(bucket),
				Key:         aws.String(filename),
				Body:        bytes.NewReader(head),
				ContentType: optionalString(contentType),
			})
			closeErr := part.Close()
			if putErr != nil {
				a.renderError(w, "Upload failed", putErr, http.StatusBadGateway)
				return
			}
			if closeErr != nil {
				a.renderError(w, "failed to close uploaded file stream", closeErr, http.StatusBadRequest)
				return
			}

			uploadedCount++
			continue
		}

		// LARGE: Multipart Upload (stream in 64MiB chunks)
		createOut, createErr := s3Client.CreateMultipartUpload(r.Context(), &s3.CreateMultipartUploadInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(filename),
			ContentType: optionalString(contentType),
		})
		if createErr != nil {
			_ = part.Close()
			a.renderError(w, "CreateMultipartUpload failed", createErr, http.StatusBadGateway)
			return
		}

		uploadID := aws.ToString(createOut.UploadId)
		var completedParts []types.CompletedPart
		partNumber := int32(1)

		abort := func(cause error, msg string) {
			_, _ = s3Client.AbortMultipartUpload(r.Context(), &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(bucket),
				Key:      aws.String(filename),
				UploadId: aws.String(uploadID),
			})
			_ = part.Close()
			a.renderError(w, msg, cause, http.StatusBadGateway)
		}

		// Helper to upload one MPU part
		uploadPart := func(data []byte, num int32) error {
			out, err := s3Client.UploadPart(r.Context(), &s3.UploadPartInput{
				Bucket:        aws.String(bucket),
				Key:           aws.String(filename),
				UploadId:      aws.String(uploadID),
				PartNumber:    aws.Int32(num),
				Body:          bytes.NewReader(data),
				ContentLength: aws.Int64(int64(len(data))),
			})
			if err != nil {
				return err
			}
			completedParts = append(completedParts, types.CompletedPart{
				ETag:       out.ETag,
				PartNumber: aws.Int32(num),
			})
			return nil
		}

		// Upload the already-read "head" as part #1 (it is > 64MiB so it's a valid non-last part)
		if err := uploadPart(head, partNumber); err != nil {
			abort(err, "UploadPart failed (initial chunk)")
			return
		}
		partNumber++

		// Stream the rest in fixed 64MiB chunks.
		buf := make([]byte, maxPartSize)
		for {
			n, readErr := io.ReadFull(part, buf)
			if readErr == nil {
				if upErr := uploadPart(buf[:n], partNumber); upErr != nil {
					abort(upErr, "UploadPart failed")
					return
				}
				partNumber++
				continue
			}
			if errors.Is(readErr, io.ErrUnexpectedEOF) {
				if n > 0 {
					if upErr := uploadPart(buf[:n], partNumber); upErr != nil {
						abort(upErr, "UploadPart failed")
						return
					}
				}
				break
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				abort(readErr, "failed while reading upload stream")
				return
			}
		}

		// Complete MPU
		_, completeErr := s3Client.CompleteMultipartUpload(r.Context(), &s3.CompleteMultipartUploadInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String(filename),
			UploadId: aws.String(uploadID),
			MultipartUpload: &types.CompletedMultipartUpload{
				Parts: completedParts,
			},
		})
		closeErr := part.Close()

		if completeErr != nil {
			abort(completeErr, "CompleteMultipartUpload failed")
			return
		}
		if closeErr != nil {
			a.renderError(w, "failed to close uploaded file stream", closeErr, http.StatusBadRequest)
			return
		}

		uploadedCount++
	}

	slog.Info("FileUpload", "Files", uploadFileNames)

	if uploadedCount == 0 {
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
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

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
}

func (a *app) handleRenameObject(w http.ResponseWriter, r *http.Request) {
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
	newKey := strings.TrimSpace(r.FormValue("new_key"))
	if bucket == "" || key == "" {
		http.Error(w, "bucket and key required", http.StatusBadRequest)
		return
	}
	if newKey == "" {
		http.Error(w, "new_key required", http.StatusBadRequest)
		return
	}
	if newKey == key {
		http.Error(w, "new_key must differ from key", http.StatusBadRequest)
		return
	}

	copySource := url.PathEscape(bucket) + "/" + url.PathEscape(key)
	_, err := s3Client.CopyObject(r.Context(), &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(newKey),
	})
	if err != nil {
		a.renderError(w, "CopyObject failed", err, http.StatusBadGateway)
		return
	}

	_, err = s3Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		a.renderError(w, "DeleteObject failed after copy", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
}

func (a *app) handlePutLifecycle(w http.ResponseWriter, r *http.Request) {
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

	ruleID := strings.TrimSpace(r.FormValue("rule_id"))
	if ruleID == "" {
		http.Error(w, "rule_id required", http.StatusBadRequest)
		return
	}

	statusStr := strings.TrimSpace(r.FormValue("rule_status"))
	if statusStr != "Enabled" && statusStr != "Disabled" {
		http.Error(w, "rule_status must be Enabled or Disabled", http.StatusBadRequest)
		return
	}

	expirationDaysStr := strings.TrimSpace(r.FormValue("expiration_days"))
	if expirationDaysStr == "" {
		http.Error(w, "expiration_days required", http.StatusBadRequest)
		return
	}
	expirationDays, err := strconv.Atoi(expirationDaysStr)
	if err != nil || expirationDays < 1 {
		http.Error(w, "expiration_days must be a positive integer", http.StatusBadRequest)
		return
	}

	prefix := strings.TrimSpace(r.FormValue("rule_prefix"))

	// Fetch existing rules so the PUT appends rather than replaces.
	var existingRules []types.LifecycleRule
	lcOut, lcErr := s3Client.GetBucketLifecycleConfiguration(r.Context(), &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if lcErr == nil {
		existingRules = lcOut.Rules
	} else if !isNoSuchLifecycleConfigurationError(lcErr) {
		a.renderError(w, "GetBucketLifecycleConfiguration failed", lcErr, http.StatusBadGateway)
		return
	}

	newRule := types.LifecycleRule{
		ID:     aws.String(ruleID),
		Status: types.ExpirationStatus(statusStr),
		Filter: &types.LifecycleRuleFilter{
			Prefix: aws.String(prefix),
		},
		Expiration: &types.LifecycleExpiration{
			Days: aws.Int32(int32(expirationDays)),
		},
	}
	rules := append(existingRules, newRule)

	_, err = s3Client.PutBucketLifecycleConfiguration(r.Context(), &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: rules,
		},
	})
	if err != nil {
		a.renderError(w, "PutBucketLifecycleConfiguration failed", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
}

func (a *app) handleDeleteLifecycle(w http.ResponseWriter, r *http.Request) {
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

	_, err := s3Client.DeleteBucketLifecycle(r.Context(), &s3.DeleteBucketLifecycleInput{Bucket: aws.String(bucket)})
	if err != nil {
		a.renderError(w, "DeleteBucketLifecycle failed", err, http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
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
