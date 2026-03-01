package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Env:
//   AWS_REGION or S3_REGION (default eu-west-1)
//   LISTEN_ADDR (default :8080)
//   AWS_ENDPOINT_URL or S3_ENDPOINT (optional for MinIO/S3-compatible)
//   S3_FORCE_PATH_STYLE ("true" commonly for MinIO)
//   AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN
//   or S3_ACCESS_KEY / S3_SECRET_KEY (optional; else default chain)

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

func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	endpoint := getenvAny("", "AWS_ENDPOINT_URL", "S3_ENDPOINT")

	var optFns []func(*config.LoadOptions) error
	optFns = append(optFns, config.WithRegion(region))

	ak := getenvAny("", "AWS_ACCESS_KEY_ID", "S3_ACCESS_KEY")
	sk := getenvAny("", "AWS_SECRET_ACCESS_KEY", "S3_SECRET_KEY")
	st := getenvAny("", "AWS_SESSION_TOKEN")
	if ak != "" && sk != "" {
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, st)))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return aws.Config{}, err
	}

	if endpoint != "" {
		ep, err := url.Parse(endpoint)
		if err != nil {
			return aws.Config{}, fmt.Errorf("invalid AWS_ENDPOINT_URL: %w", err)
		}
		awsCfg.BaseEndpoint = aws.String(ep.String())
	}

	return awsCfg, nil
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getenvAny(def string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return def
}

// ---------------- Index (buckets + forms) ----------------

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()

	out, err := a.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		a.renderError(w, "ListBuckets failed", err, http.StatusBadGateway)
		return
	}

	type bucketRow struct {
		Name         string
		CreationDate string
		BrowseURL    string
	}

	rows := make([]bucketRow, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		name := aws.ToString(b.Name)
		cd := ""
		if b.CreationDate != nil {
			cd = b.CreationDate.Format(time.RFC3339)
		}
		rows = append(rows, bucketRow{
			Name:         name,
			CreationDate: cd,
			BrowseURL:    fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(name)),
		})
	}

	a.render(w, "index", map[string]any{
		"Title":   "Buckets",
		"Buckets": rows,
	})
}

// Go to bucket by name (works for buckets you can access but do not own)
func (a *app) handleGoToBucket(w http.ResponseWriter, r *http.Request) {
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

	// Optional validation (friendly errors)
	ctx := r.Context()
	_, err := a.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		a.renderError(w, "HeadBucket failed (bucket may not exist or you lack access)", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(bucket)), http.StatusSeeOther)
}

// ---------------- Bucket browse ----------------

// /bucket/{bucket}?prefix=...&max=200&token=...
func (a *app) handleBucketBrowse(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/bucket/")
	if p == "" || strings.Contains(p, "/") {
		http.NotFound(w, r)
		return
	}
	bucket := p

	prefix := r.URL.Query().Get("prefix")
	maxKeys := parseIntClamp(r.URL.Query().Get("max"), 200, 1, 1000)
	token := r.URL.Query().Get("token")

	ctx := r.Context()
	out, err := a.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:            aws.String(bucket),
		Prefix:            aws.String(prefix),
		Delimiter:         aws.String("/"),
		MaxKeys:           aws.Int32(int32(maxKeys)),
		ContinuationToken: optionalString(token),
	})
	if err != nil {
		a.renderError(w, "ListObjectsV2 failed", err, http.StatusBadGateway)
		return
	}

	type prefixRow struct{ Name, URL string }
	type objRow struct {
		Key, Size, LastModified, ETag, DetailsURL, DownloadURL string
	}

	crumbs := breadcrumbs(bucket, prefix)
	upPrefix := parentPrefix(prefix)

	folders := make([]prefixRow, 0, len(out.CommonPrefixes))
	for _, cp := range out.CommonPrefixes {
		pfx := aws.ToString(cp.Prefix)
		folders = append(folders, prefixRow{
			Name: strings.TrimSuffix(path.Base(pfx), "/") + "/",
			URL:  fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(pfx)),
		})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })

	objects := make([]objRow, 0, len(out.Contents))
	for _, o := range out.Contents {
		key := aws.ToString(o.Key)

		// skip directory marker object that equals the prefix and ends with '/'
		if key == prefix && strings.HasSuffix(key, "/") {
			continue
		}

		objects = append(objects, objRow{
			Key:          key,
			Size:         humanBytes(aws.ToInt64(o.Size)),
			LastModified: timeStr(o.LastModified),
			ETag:         strings.Trim(aws.ToString(o.ETag), `"`),
			DetailsURL:   fmt.Sprintf("/object/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
			DownloadURL:  fmt.Sprintf("/download/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
		})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })

	nextToken := ""
	if aws.ToBool(out.IsTruncated) && out.NextContinuationToken != nil {
		nextToken = aws.ToString(out.NextContinuationToken)
	}

	a.render(w, "bucket", map[string]any{
		"Title":    "Browse bucket",
		"Bucket":   bucket,
		"Prefix":   prefix,
		"Crumbs":   crumbs,
		"UpPrefix": upPrefix,

		"Folders": folders,
		"Objects": objects,

		"MaxKeys":     maxKeys,
		"HasNext":     nextToken != "",
		"NextPageURL": nextPageURL(bucket, prefix, maxKeys, nextToken),

		"UploadAction":     "/upload",
		"DeleteBucketPOST": "/bucket/delete",
	})
}

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

// /object/{bucket}/{key...}
func (a *app) handleObject(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/object/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	bucket, key := parts[0], parts[1]

	ctx := r.Context()

	head, err := a.s3.HeadObject(ctx, &s3.HeadObjectInput{
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
	tagOut, tagErr := a.s3.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
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

	if head.Expires != nil {
		addSys("Expires", head.Expires.Format(time.RFC3339))
	}
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
	backURL := fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(parent))

	a.render(w, "object", map[string]any{
		"Title":        "Object details",
		"Bucket":       bucket,
		"Key":          key,
		"Size":         humanBytes(aws.ToInt64(head.ContentLength)),
		"ContentType":  aws.ToString(head.ContentType),
		"LastModified": timeStr(head.LastModified),
		"ETag":         strings.Trim(aws.ToString(head.ETag), `"`),
		"StorageClass": string(head.StorageClass),

		"UserMetadata":   userMeta,
		"SystemMetadata": sys,
		"Tags":           tags,
		"TagError":       tagErrStr,

		"BackURL":          backURL,
		"DownloadURL":      fmt.Sprintf("/download/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
		"DeleteObjectPOST": "/object/delete",
	})
}

// ---------------- Download ----------------

func (a *app) handleDownload(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/download/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	bucket, key := parts[0], parts[1]

	ctx := r.Context()
	out, err := a.s3.GetObject(ctx, &s3.GetObjectInput{
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

	f, hdr, err := r.FormFile("file")
	if err != nil {
		a.renderError(w, "file is required", err, http.StatusBadRequest)
		return
	}
	defer f.Close()

	key := strings.TrimPrefix(prefix, "/")
	if key != "" && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	if overrideKey != "" {
		key += strings.TrimPrefix(overrideKey, "/")
	} else {
		key += hdr.Filename
	}
	key = strings.TrimPrefix(key, "/")

	contentType := hdr.Header.Get("Content-Type")

	ctx := r.Context()
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: optionalString(contentType),
	})
	if err != nil {
		a.renderError(w, "PutObject failed", err, http.StatusBadGateway)
		return
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

// ---------------- Helpers ----------------

func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return aws.String(s)
}

func parseIntClamp(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func timeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
	return fmt.Sprintf("%.2f %s", value, suffix)
}

func parentPrefix(pfx string) string {
	if pfx == "" {
		return ""
	}
	trim := strings.TrimSuffix(pfx, "/")
	i := strings.LastIndex(trim, "/")
	if i < 0 {
		return ""
	}
	return trim[:i+1]
}

type crumb struct {
	Name string
	URL  string
}

func breadcrumbs(bucket, prefix string) []crumb {
	out := []crumb{{Name: bucket, URL: fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(bucket))}}
	if prefix == "" {
		return out
	}
	parts := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
	cur := ""
	for _, p := range parts {
		cur += p + "/"
		out = append(out, crumb{
			Name: p,
			URL:  fmt.Sprintf("/bucket/%s?prefix=%s", url.PathEscape(bucket), url.QueryEscape(cur)),
		})
	}
	return out
}

func nextPageURL(bucket, prefix string, maxKeys int, token string) string {
	q := url.Values{}
	q.Set("prefix", prefix)
	q.Set("max", strconv.Itoa(maxKeys))
	q.Set("token", token)
	return fmt.Sprintf("/bucket/%s?%s", url.PathEscape(bucket), q.Encode())
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, `"`, "_")
	s = strings.ReplaceAll(s, "\n", "_")
	s = strings.ReplaceAll(s, "\r", "_")
	if s == "" {
		return "download"
	}
	return s
}

// ---------------- Templates ----------------

func newTemplates() *template.Template {
	funcs := template.FuncMap{
		"sub1": func(i int) int { return i - 1 },
		"lenCrumbs": func(v any) int {
			if c, ok := v.([]crumb); ok {
				return len(c)
			}
			return 0
		},
		"hasErr": func(s string) bool { return strings.TrimSpace(s) != "" },
	}
	return template.Must(template.New("base").Funcs(funcs).Parse(htmlTemplates))
}

func (a *app) render(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data["Now"] = time.Now().Format(time.RFC3339)
	if err := a.tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *app) renderError(w http.ResponseWriter, msg string, err error, code int) {
	log.Printf("%s: %v", msg, err)
	w.WriteHeader(code)

	// render a useful error page
	_ = a.tpl.ExecuteTemplate(w, "error", map[string]any{
		"Title":   "Error",
		"Message": msg,
		"Error":   err.Error(),
		"Code":    code,
		"Now":     time.Now().Format(time.RFC3339),
	})
}

const htmlTemplates = `
{{define "layout-start"}}
<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Title}}</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif; margin: 24px; }
    a { text-decoration: none; }
    a:hover { text-decoration: underline; }
    .muted { color: #666; }
    .row { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
    .card { border: 1px solid #ddd; border-radius: 10px; padding: 14px; margin: 12px 0; }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid #eee; padding: 8px; text-align: left; vertical-align: top; }
    th { background: #fafafa; }
    code { background: #f5f5f5; padding: 2px 4px; border-radius: 4px; }
    .btn { display:inline-block; padding: 8px 10px; border:1px solid #ddd; border-radius: 8px; background: #fff; cursor: pointer;}
    input[type="text"]{ padding:8px; border:1px solid #ddd; border-radius: 8px; min-width: 280px;}
    input[type="file"]{ padding:6px; }
    .danger { border-color:#f1c3c3; }
    .warn { border: 1px solid #f0d7a1; background: #fff8e6; padding: 10px; border-radius: 10px; }
  </style>
</head>
<body>
  <div class="row">
    <h2 style="margin:0;"><a href="/">S3 Viewer</a></h2>
    <span class="muted">Generated: {{.Now}}</span>
  </div>
  <hr/>
{{end}}

{{define "layout-end"}}
</body>
</html>
{{end}}

{{define "index"}}
{{template "layout-start" .}}
  <div class="card">
    <h3 style="margin-top:0;">Go to bucket</h3>
    <form method="post" action="/bucket/goto" class="row">
      <input type="text" name="bucket" placeholder="bucket-name" required />
      <button class="btn" type="submit">Open</button>
    </form>
    <p class="muted" style="margin-bottom:0;">
      Use this for buckets you have access to but don’t own (so they won’t appear in the list).
    </p>
  </div>

  <div class="card">
    <h3 style="margin-top:0;">Create bucket</h3>
    <form method="post" action="/bucket/create" class="row">
      <input type="text" name="bucket" placeholder="bucket-name" required />
      <button class="btn" type="submit">Create</button>
    </form>
    <p class="muted" style="margin-bottom:0;">Bucket naming rules depend on provider; keep it DNS-safe.</p>
  </div>

  <h3>Buckets (owned)</h3>
  <div class="card">
    <table>
      <thead><tr><th>Name</th><th>Created</th></tr></thead>
      <tbody>
      {{range .Buckets}}
        <tr>
          <td><a href="{{.BrowseURL}}">{{.Name}}</a></td>
          <td class="muted">{{.CreationDate}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>
{{template "layout-end" .}}
{{end}}

{{define "bucket"}}
{{template "layout-start" .}}
  <div class="row">
    <h3 style="margin:0;">Bucket: <code>{{.Bucket}}</code></h3>
    {{if .Prefix}}<span class="muted">Prefix: <code>{{.Prefix}}</code></span>{{end}}
  </div>

  <div class="card">
    <div class="row">
      <strong>Path:</strong>
      {{ $n := lenCrumbs .Crumbs }}
      {{range $i, $c := .Crumbs}}
        <a href="{{$c.URL}}">{{$c.Name}}</a>{{if lt $i (sub1 $n)}} / {{end}}
      {{end}}
    </div>

    <div class="row" style="margin-top:10px;">
      {{if .UpPrefix}}
        <a class="btn" href="/bucket/{{.Bucket}}?prefix={{.UpPrefix}}">⬆ Up</a>
      {{end}}

      <form method="post" action="{{.DeleteBucketPOST}}" onsubmit="return confirm('Delete bucket {{.Bucket}}? Bucket must be EMPTY.');">
        <input type="hidden" name="bucket" value="{{.Bucket}}" />
        <button class="btn danger" type="submit">Delete bucket</button>
      </form>
    </div>
  </div>

  <div class="card">
    <h4 style="margin-top:0;">Upload file</h4>
    <form method="post" action="{{.UploadAction}}" enctype="multipart/form-data">
      <input type="hidden" name="bucket" value="{{.Bucket}}" />
      <input type="hidden" name="prefix" value="{{.Prefix}}" />
      <div class="row">
        <input type="file" name="file" required />
        <input type="text" name="key" placeholder="optional: override key (e.g. docs/readme.txt)" />
        <button class="btn" type="submit">Upload</button>
      </div>
      <p class="muted" style="margin-bottom:0;">Uploads into current prefix unless you override key.</p>
    </form>
  </div>

  <div class="card">
    <h4>Folders</h4>
    {{if not .Folders}}<div class="muted">No folders.</div>{{end}}
    <ul>
      {{range .Folders}}
        <li><a href="{{.URL}}">{{.Name}}</a></li>
      {{end}}
    </ul>

    <h4>Objects</h4>
    {{if not .Objects}}<div class="muted">No objects.</div>{{end}}
    <table>
      <thead><tr><th>Key</th><th>Size</th><th>Last modified</th><th></th></tr></thead>
      <tbody>
        {{range .Objects}}
          <tr>
            <td><a href="{{.DetailsURL}}"><code>{{.Key}}</code></a></td>
            <td>{{.Size}}</td>
            <td class="muted">{{.LastModified}}</td>
            <td><a class="btn" href="{{.DownloadURL}}">Download</a></td>
          </tr>
        {{end}}
      </tbody>
    </table>

    {{if .HasNext}}
      <p><a class="btn" href="{{.NextPageURL}}">Next page →</a></p>
    {{end}}
  </div>
{{template "layout-end" .}}
{{end}}

{{define "object"}}
{{template "layout-start" .}}
  <h3>Object</h3>
  <div class="card">
    <p><strong>Bucket:</strong> <code>{{.Bucket}}</code></p>
    <p><strong>Key:</strong> <code>{{.Key}}</code></p>
    <p><strong>Size:</strong> {{.Size}}</p>
    <p><strong>Content-Type:</strong> <code>{{.ContentType}}</code></p>
    <p><strong>Last modified:</strong> <span class="muted">{{.LastModified}}</span></p>
    <p><strong>ETag:</strong> <code>{{.ETag}}</code></p>
    <p><strong>Storage class:</strong> <code>{{.StorageClass}}</code></p>

    <div class="row" style="margin-top:12px;">
      <a class="btn" href="{{.BackURL}}">← Back</a>
      <a class="btn" href="{{.DownloadURL}}">Download</a>

      <form method="post" action="{{.DeleteObjectPOST}}" onsubmit="return confirm('Delete object?');">
        <input type="hidden" name="bucket" value="{{.Bucket}}" />
        <input type="hidden" name="key" value="{{.Key}}" />
        <button class="btn danger" type="submit">Delete object</button>
      </form>
    </div>
  </div>

  <div class="card">
    <h4 style="margin-top:0;">Tags</h4>
    {{if hasErr .TagError}}
      <div class="warn">
        <strong>Could not read tags</strong><br/>
        <span class="muted">{{.TagError}}</span>
      </div>
    {{end}}

    {{if not .Tags}}
      <div class="muted">No tags.</div>
    {{else}}
      <table>
        <thead><tr><th>Key</th><th>Value</th></tr></thead>
        <tbody>
          {{range .Tags}}
            <tr><td><code>{{.K}}</code></td><td><code>{{.V}}</code></td></tr>
          {{end}}
        </tbody>
      </table>
    {{end}}
  </div>

  <div class="card">
    <h4 style="margin-top:0;">User metadata (x-amz-meta-*)</h4>
    {{if not .UserMetadata}}
      <div class="muted">No user metadata.</div>
    {{else}}
      <table>
        <thead><tr><th>Key</th><th>Value</th></tr></thead>
        <tbody>
          {{range .UserMetadata}}
            <tr><td><code>{{.K}}</code></td><td><code>{{.V}}</code></td></tr>
          {{end}}
        </tbody>
      </table>
    {{end}}
  </div>

  <div class="card">
    <h4 style="margin-top:0;">System metadata</h4>
    {{if not .SystemMetadata}}
      <div class="muted">No system metadata.</div>
    {{else}}
      <table>
        <thead><tr><th>Header</th><th>Value</th></tr></thead>
        <tbody>
          {{range .SystemMetadata}}
            <tr><td><code>{{.K}}</code></td><td><code>{{.V}}</code></td></tr>
          {{end}}
        </tbody>
      </table>
    {{end}}
  </div>
{{template "layout-end" .}}
{{end}}

{{define "error"}}
{{template "layout-start" .}}
  <h3>Error ({{.Code}})</h3>
  <div class="card">
    <p><strong>{{.Message}}</strong></p>
    <pre style="white-space:pre-wrap;">{{.Error}}</pre>
    <p><a class="btn" href="/">Home</a></p>
  </div>
{{template "layout-end" .}}
{{end}}
`

// prevent unused imports in case you remove something
var _ = errors.New
