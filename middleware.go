package main

import (
	"log"
	"net/http"
	"strings"
	"time"
)

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// Sanitize method and path before logging to prevent log injection.
		method := strings.Map(keepPrintable, r.Method)
		path := strings.Map(keepPrintable, r.URL.EscapedPath())
		log.Printf("%s %s completed (%s)", method, path, time.Since(start)) // #nosec G706 -- method and path are sanitized by keepPrintable above
	})
}

// keepPrintable replaces control characters (including Unicode line/paragraph
// separators and C1 controls) with '_' for safe logging.
func keepPrintable(r rune) rune {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 {
		return '_'
	}
	return r
}
