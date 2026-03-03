package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ---------------- Helpers ----------------

const (
	maxFormBodyBytes int64 = 1 << 20 // 1 MiB
)

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

func bucketBrowseURL(bucket, search, token string, prevTokens []string) string {
	q := url.Values{}
	if strings.TrimSpace(search) != "" {
		q.Set("search", search)
	}
	if strings.TrimSpace(token) != "" {
		q.Set("token", token)
	}
	for _, t := range prevTokens {
		if strings.TrimSpace(t) != "" {
			q.Add("prev", t)
		}
	}
	if len(q) == 0 {
		return fmt.Sprintf("/bucket/view/%s", url.PathEscape(bucket))
	}
	return fmt.Sprintf("/bucket/view/%s?%s", url.PathEscape(bucket), q.Encode())
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
