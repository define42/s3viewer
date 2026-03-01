package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ---------------- Object details (tags + metadata) ----------------

type kv struct {
	K string
	V string
}

func mapToKVs(m map[string]string) []kv {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]kv, 0, len(m))
	for _, k := range keys {
		out = append(out, kv{K: k, V: m[k]})
	}
	return out
}

func tagsToKVs(tags []types.Tag) []kv {
	if len(tags) == 0 {
		return nil
	}
	out := make([]kv, 0, len(tags))
	for _, t := range tags {
		out = append(out, kv{K: aws.ToString(t.Key), V: aws.ToString(t.Value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// /object/view/{bucket}/{key...}
func (a *app) handleObject(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/object/view/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	bucket, key := parts[0], parts[1]

	head, err := s3Client.HeadObject(r.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		a.renderError(w, "HeadObject failed", err, http.StatusBadGateway)
		return
	}

	// User-defined metadata (x-amz-meta-*) is in head.Metadata
	userMeta := mapToKVs(head.Metadata)

	// Tags (may require s3:GetObjectTagging; treat errors as non-fatal)
	var tags []kv
	var tagErrStr string
	tagOut, tagErr := s3Client.GetObjectTagging(r.Context(), &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if tagErr == nil {
		tags = tagsToKVs(tagOut.TagSet)
	} else {
		tagErrStr = tagErr.Error()
		log.Printf("GetObjectTagging failed (non-fatal): %v", tagErr)
	}

	// "System metadata"/headers you often care about
	sys := []kv{}
	addSys := func(k, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			sys = append(sys, kv{K: k, V: v})
		}
	}

	addSys("Cache-Control", aws.ToString(head.CacheControl))
	addSys("Content-Disposition", aws.ToString(head.ContentDisposition))
	addSys("Content-Encoding", aws.ToString(head.ContentEncoding))
	addSys("Content-Language", aws.ToString(head.ContentLanguage))
	addSys("Website-Redirect-Location", aws.ToString(head.WebsiteRedirectLocation))

	addSys("Expires", aws.ToString(head.ExpiresString))
	if head.VersionId != nil {
		addSys("VersionId", aws.ToString(head.VersionId))
	}
	if head.ServerSideEncryption != "" {
		addSys("Server-Side-Encryption", string(head.ServerSideEncryption))
	}
	if head.SSEKMSKeyId != nil {
		addSys("SSE-KMS-Key-Id", aws.ToString(head.SSEKMSKeyId))
	}
	if head.ReplicationStatus != "" {
		addSys("Replication-Status", string(head.ReplicationStatus))
	}
	if head.ObjectLockMode != "" {
		addSys("Object-Lock-Mode", string(head.ObjectLockMode))
	}
	if head.ObjectLockRetainUntilDate != nil {
		addSys("Object-Lock-Retain-Until", head.ObjectLockRetainUntilDate.Format(time.RFC3339))
	}
	if head.ObjectLockLegalHoldStatus != "" {
		addSys("Object-Lock-Legal-Hold", string(head.ObjectLockLegalHoldStatus))
	}
	sort.Slice(sys, func(i, j int) bool { return sys[i].K < sys[j].K })

	parent := parentPrefix(key)
	backURL := fmt.Sprintf("/bucket/view/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(parent))

	a.render(w, "object", map[string]any{
		"Title":           "Object details",
		"Bucket":          bucket,
		"Key":             key,
		"Size":            humanBytes(aws.ToInt64(head.ContentLength)),
		"ContentType":     aws.ToString(head.ContentType),
		"LastModified":    timeStr(head.LastModified),
		"ETag":            strings.Trim(aws.ToString(head.ETag), `"`),
		"StorageClass":    string(head.StorageClass),
		"IsAuthenticated": true,

		"UserMetadata":   userMeta,
		"SystemMetadata": sys,
		"Tags":           tags,
		"TagError":       tagErrStr,

		"BackURL":          backURL,
		"DownloadURL":      fmt.Sprintf("/object/download/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
		"DeleteObjectPOST": fmt.Sprintf("/object/delete/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
		"PresignPOST":      fmt.Sprintf("/object/presign/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
	})
}

// ---------------- Presigned URL ----------------

// /object/presign/{bucket}/{key...}
// GET or POST: generate a presigned download URL with configurable expiry.
// POST form field: "minutes" (integer, 1–10080, default 60).
func (a *app) handlePresign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/object/presign/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	bucket, key := parts[0], parts[1]

	expiry := time.Hour // default: 1 hour
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			a.renderError(w, "ParseForm failed", err, http.StatusBadRequest)
			return
		}
		minutes := parseIntClamp(r.FormValue("minutes"), defaultPresignMinutes, minPresignMinutes, maxPresignMinutes)
		expiry = time.Duration(minutes) * time.Minute
	}

	presignClient := s3.NewPresignClient(s3Client)
	presigned, err := presignClient.PresignGetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		a.renderError(w, "PresignGetObject failed", err, http.StatusBadGateway)
		return
	}

	backURL := fmt.Sprintf("/object/view/%s/%s", url.PathEscape(bucket), url.PathEscape(key))
	a.render(w, "presign", map[string]any{
		"Title":           "Presigned URL",
		"Bucket":          bucket,
		"Key":             key,
		"PresignURL":      presigned.URL,
		"ExpiresIn":       expiry.String(),
		"BackURL":         backURL,
		"IsAuthenticated": true,
	})
}

// ---------------- Download ----------------

func (a *app) handleDownload(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/object/download/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	bucket, key := parts[0], parts[1]
	out, err := s3Client.GetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		a.renderError(w, "GetObject failed", err, http.StatusBadGateway)
		return
	}
	defer out.Body.Close()

	filename := path.Base(key)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(filename)))
	if out.ContentType != nil {
		w.Header().Set("Content-Type", aws.ToString(out.ContentType))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if out.ContentLength != nil && *out.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(*out.ContentLength, 10))
	}

	_, _ = io.Copy(w, out.Body)
}
