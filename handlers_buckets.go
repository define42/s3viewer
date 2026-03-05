package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gorilla/mux"
)

const bucketPageSize = 10

func isNoSuchTagSetError(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchTagSet"
}

func isNoSuchLifecycleConfigurationError(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchLifecycleConfiguration"
}

func isNoSuchBucketPolicyError(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucketPolicy"
}

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
			BrowseURL:    fmt.Sprintf("/bucket/view/%s", url.PathEscape(name)),
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

	http.Redirect(w, r, fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket)), http.StatusSeeOther)
}

// ---------------- Bucket browse ----------------

// /bucket/view/{bucket}?search=...&token=...&prev=...
func (a *app) handleBucketBrowse(w http.ResponseWriter, r *http.Request) {
	bucketEscaped := mux.Vars(r)["bucket"]
	if bucketEscaped == "" {
		http.NotFound(w, r)
		return
	}
	bucket, err := url.PathUnescape(bucketEscaped)
	if err != nil || bucket == "" {
		http.NotFound(w, r)
		return
	}
	s3Client, ok := a.authenticatedS3Client(w, r)
	if !ok {
		return
	}

	search := r.URL.Query().Get("search")
	listFilter := search
	token := r.URL.Query().Get("token")
	prevTokens := append([]string(nil), r.URL.Query()["prev"]...)

	bucketTags := []kv(nil)
	bucketTagError := ""
	bucketTagOut, bucketTagErr := s3Client.GetBucketTagging(r.Context(), &s3.GetBucketTaggingInput{
		Bucket: aws.String(bucket),
	})
	if bucketTagErr == nil {
		bucketTags = tagsToKVs(bucketTagOut.TagSet)
	} else if !isNoSuchTagSetError(bucketTagErr) {
		bucketTagError = "unavailable"
		slog.Warn("GetBucketTagging failed in bucket browse", "bucket", bucket, "error", bucketTagErr)
	}

	type lifecycleRuleRow struct {
		ID          string
		Status      string
		Prefix      string
		Expiration  string
		Transitions []string
		AbortDays   string
	}

	var lifecycleRules []lifecycleRuleRow
	lifecycleError := ""
	lcOut, lcErr := s3Client.GetBucketLifecycleConfiguration(r.Context(), &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if lcErr == nil {
		for _, rule := range lcOut.Rules {
			row := lifecycleRuleRow{
				ID:     aws.ToString(rule.ID),
				Status: string(rule.Status),
			}
			if rule.Filter != nil {
				if rule.Filter.Prefix != nil {
					row.Prefix = aws.ToString(rule.Filter.Prefix)
				} else if rule.Filter.And != nil && rule.Filter.And.Prefix != nil {
					row.Prefix = aws.ToString(rule.Filter.And.Prefix)
				}
			}
			if rule.Expiration != nil {
				if rule.Expiration.Days != nil {
					row.Expiration = strconv.Itoa(int(aws.ToInt32(rule.Expiration.Days))) + " days"
				} else if rule.Expiration.Date != nil {
					row.Expiration = rule.Expiration.Date.Format(time.RFC3339)
				} else if rule.Expiration.ExpiredObjectDeleteMarker != nil && aws.ToBool(rule.Expiration.ExpiredObjectDeleteMarker) {
					row.Expiration = "delete marker"
				}
			}
			for _, tr := range rule.Transitions {
				desc := string(tr.StorageClass)
				if tr.Days != nil {
					desc += " after " + strconv.Itoa(int(aws.ToInt32(tr.Days))) + " days"
				} else if tr.Date != nil {
					desc += " on " + tr.Date.Format(time.RFC3339)
				}
				row.Transitions = append(row.Transitions, desc)
			}
			if rule.AbortIncompleteMultipartUpload != nil && rule.AbortIncompleteMultipartUpload.DaysAfterInitiation != nil {
				row.AbortDays = strconv.Itoa(int(aws.ToInt32(rule.AbortIncompleteMultipartUpload.DaysAfterInitiation))) + " days"
			}
			lifecycleRules = append(lifecycleRules, row)
		}
	} else if !isNoSuchLifecycleConfigurationError(lcErr) {
		lifecycleError = "unavailable"
		slog.Warn("GetBucketLifecycleConfiguration failed in bucket browse", "bucket", bucket, "error", lcErr)
	}

	bucketPolicy := ""
	bucketPolicyError := ""
	policyOut, policyErr := s3Client.GetBucketPolicy(r.Context(), &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if policyErr == nil {
		bucketPolicy = aws.ToString(policyOut.Policy)
	} else if !isNoSuchBucketPolicyError(policyErr) {
		bucketPolicyError = "unavailable"
		slog.Warn("GetBucketPolicy failed in bucket browse", "bucket", bucket, "error", policyErr)
	}

	out, err := s3Client.ListObjectsV2(r.Context(), &s3.ListObjectsV2Input{
		Bucket:            aws.String(bucket),
		Prefix:            aws.String(listFilter),
		MaxKeys:           aws.Int32(bucketPageSize),
		ContinuationToken: optionalString(token),
	})
	if err != nil {
		a.renderError(w, "ListObjectsV2 failed", err, http.StatusBadGateway)
		return
	}

	type objRow struct {
		Key, Size, LastModified, ETag, DetailsURL, DownloadURL, DeleteURL string
		Metadata                                                          []kv
		Tags                                                              []kv
		MetadataError                                                     string
		TagError                                                          string
	}

	browseAction := fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket))
	clearSearchURL := bucketBrowseURL(bucket, "", "", nil)

	objects := make([]objRow, 0, len(out.Contents))
	for _, o := range out.Contents {
		key := aws.ToString(o.Key)

		// skip directory marker object that equals the search filter and ends with '/'
		if key == listFilter && strings.HasSuffix(key, "/") {
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
			slog.Warn("HeadObject failed in bucket browse", "bucket", bucket, "key", key, "error", headErr)
		}

		tagOut, tagErr := s3Client.GetObjectTagging(r.Context(), &s3.GetObjectTaggingInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if tagErr == nil {
			tags = tagsToKVs(tagOut.TagSet)
		} else {
			tagErrStr = "unavailable"
			slog.Warn("GetObjectTagging failed in bucket browse", "bucket", bucket, "key", key, "error", tagErr)
		}

		objects = append(objects, objRow{
			Key:           key,
			Size:          humanBytes(aws.ToInt64(o.Size)),
			LastModified:  timeStr(o.LastModified),
			ETag:          strings.Trim(aws.ToString(o.ETag), `"`),
			DetailsURL:    fmt.Sprintf("/object/view/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
			DownloadURL:   fmt.Sprintf("/object/download/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
			DeleteURL:     fmt.Sprintf("/object/delete/%s/%s", url.PathEscape(bucket), url.PathEscape(key)),
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
		prevPageURL = bucketBrowseURL(bucket, search, prevToken, prevHistory)
	}

	hasNext := nextToken != ""
	nextPageURL := ""
	if hasNext {
		nextHistory := append([]string(nil), prevTokens...)
		if token != "" {
			nextHistory = append(nextHistory, token)
		}
		nextPageURL = bucketBrowseURL(bucket, search, nextToken, nextHistory)
	}

	a.render(w, "bucket", map[string]any{
		"Title":           "Browse bucket",
		"Bucket":          bucket,
		"Search":          search,
		"BrowseAction":    browseAction,
		"ClearSearchURL":  clearSearchURL,
		"BucketTags":      bucketTags,
		"BucketTagError":  bucketTagError,
		"IsAuthenticated": true,

		"Objects": objects,

		"HasPrev":     hasPrev,
		"PrevPageURL": prevPageURL,
		"HasNext":     hasNext,
		"NextPageURL": nextPageURL,

		"UploadAction":     fmt.Sprintf("/object/upload/%s", url.PathEscape(bucket)),
		"DeleteBucketPOST": fmt.Sprintf("/bucket/delete/%s", url.PathEscape(bucket)),

		"LifecycleRules":      lifecycleRules,
		"LifecycleError":      lifecycleError,
		"DeleteLifecyclePOST": fmt.Sprintf("/bucket/lifecycle/delete/%s", url.PathEscape(bucket)),
		"PutLifecyclePOST":    fmt.Sprintf("/bucket/lifecycle/put/%s", url.PathEscape(bucket)),

		"BucketPolicy":      bucketPolicy,
		"BucketPolicyError": bucketPolicyError,
		"DeletePolicyPOST":  fmt.Sprintf("/bucket/policy/delete/%s", url.PathEscape(bucket)),
		"PutPolicyPOST":     fmt.Sprintf("/bucket/policy/put/%s", url.PathEscape(bucket)),
	})
}
