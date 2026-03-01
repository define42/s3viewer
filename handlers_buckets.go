package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const bucketPageSize = 10

// ---------------- Index (buckets + forms) ----------------

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}

	out, err := s3Client.ListBuckets(r.Context(), &s3.ListBucketsInput{})
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
		"Title":           "Buckets",
		"Buckets":         rows,
		"IsAuthenticated": true,
	})
}

// Go to bucket by name (works for buckets you can access but do not own)
func (a *app) handleGoToBucket(w http.ResponseWriter, r *http.Request) {
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

	// Optional validation (friendly errors)
	_, err := s3Client.HeadBucket(r.Context(), &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		a.renderError(w, "HeadBucket failed (bucket may not exist or you lack access)", err, http.StatusBadGateway)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/bucket/%s?prefix=", url.PathEscape(bucket)), http.StatusSeeOther)
}

// ---------------- Bucket browse ----------------

// /bucket/{bucket}?prefix=...&token=...&prev=...
func (a *app) handleBucketBrowse(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/bucket/")
	if p == "" || strings.Contains(p, "/") {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}
	bucket := p

	prefix := r.URL.Query().Get("prefix")
	token := r.URL.Query().Get("token")
	prevTokens := append([]string(nil), r.URL.Query()["prev"]...)

	out, err := s3Client.ListObjectsV2(r.Context(), &s3.ListObjectsV2Input{
		Bucket:            aws.String(bucket),
		Prefix:            aws.String(prefix),
		Delimiter:         aws.String("/"),
		MaxKeys:           aws.Int32(bucketPageSize),
		ContinuationToken: optionalString(token),
	})
	if err != nil {
		a.renderError(w, "ListObjectsV2 failed", err, http.StatusBadGateway)
		return
	}

	type prefixRow struct{ Name, URL string }
	type objRow struct {
		Key, Size, LastModified, ETag, DetailsURL, DownloadURL string
		Metadata                                               []kv
		Tags                                                   []kv
		MetadataError                                          string
		TagError                                               string
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

		metadata := []kv(nil)
		tags := []kv(nil)
		metadataErrStr := ""
		tagErrStr := ""

		head, headErr := s3Client.HeadObject(r.Context(), &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if headErr == nil {
			metadata = mapToKVs(head.Metadata)
		} else {
			metadataErrStr = "unavailable"
			log.Printf("HeadObject failed in bucket browse (non-fatal): %v", headErr)
		}

		tagOut, tagErr := s3Client.GetObjectTagging(r.Context(), &s3.GetObjectTaggingInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if tagErr == nil {
			tags = tagsToKVs(tagOut.TagSet)
		} else {
			tagErrStr = "unavailable"
			log.Printf("GetObjectTagging failed in bucket browse (non-fatal): %v", tagErr)
		}

		objects = append(objects, objRow{
			Key:           key,
			Size:          humanBytes(aws.ToInt64(o.Size)),
			LastModified:  timeStr(o.LastModified),
			ETag:          strings.Trim(aws.ToString(o.ETag), `"`),
			DetailsURL:    fmt.Sprintf("/object/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
			DownloadURL:   fmt.Sprintf("/download/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
			Metadata:      metadata,
			Tags:          tags,
			MetadataError: metadataErrStr,
			TagError:      tagErrStr,
		})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })

	nextToken := ""
	if aws.ToBool(out.IsTruncated) && out.NextContinuationToken != nil {
		nextToken = aws.ToString(out.NextContinuationToken)
	}

	hasPrev := token != ""
	prevPageURL := ""
	if hasPrev {
		prevToken := ""
		prevHistory := []string{}
		if len(prevTokens) > 0 {
			prevToken = prevTokens[len(prevTokens)-1]
			prevHistory = prevTokens[:len(prevTokens)-1]
		}
		prevPageURL = bucketBrowseURL(bucket, prefix, prevToken, prevHistory)
	}

	hasNext := nextToken != ""
	nextPageURL := ""
	if hasNext {
		nextHistory := append([]string(nil), prevTokens...)
		if token != "" {
			nextHistory = append(nextHistory, token)
		}
		nextPageURL = bucketBrowseURL(bucket, prefix, nextToken, nextHistory)
	}

	a.render(w, "bucket", map[string]any{
		"Title":           "Browse bucket",
		"Bucket":          bucket,
		"Prefix":          prefix,
		"Crumbs":          crumbs,
		"UpPrefix":        upPrefix,
		"IsAuthenticated": true,

		"Folders": folders,
		"Objects": objects,

		"HasPrev":     hasPrev,
		"PrevPageURL": prevPageURL,
		"HasNext":     hasNext,
		"NextPageURL": nextPageURL,

		"UploadAction":     fmt.Sprintf("/object/upload/%s", url.PathEscape(bucket)),
		"DeleteBucketPOST": fmt.Sprintf("/bucket/delete/%s", url.PathEscape(bucket)),
	})
}
