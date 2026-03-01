package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	testcontainers "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestIntegrationMinIOLoginCreateAndUpload(t *testing.T) {
	ctx := context.Background()

	minio, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "minio/minio:latest",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			},
			Cmd: []string{"server", "/data", "--console-address", ":9001"},
			WaitingFor: wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start minio container: %v", err)
	}
	t.Cleanup(func() {
		_ = minio.Terminate(ctx)
	})

	host, err := minio.Host(ctx)
	if err != nil {
		t.Fatalf("get minio host: %v", err)
	}
	port, err := minio.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("get minio mapped port: %v", err)
	}
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	a := newIntegrationTestApp(endpoint)
	srv := httptest.NewServer(newIntegrationTestMux(a))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	loginResp := postForm(t, client, srv.URL+"/login", url.Values{
		"access_key": {"minioadmin"},
		"secret_key": {"minioadmin"},
	})
	requireStatus(t, loginResp, http.StatusSeeOther)
	requireLocation(t, loginResp, "/")
	discardAndClose(t, loginResp)

	bucket := fmt.Sprintf("integration-bucket-%d", time.Now().UnixNano())
	createResp := postForm(t, client, srv.URL+"/bucket/create", url.Values{
		"bucket": {bucket},
	})
	requireStatus(t, createResp, http.StatusSeeOther)
	discardAndClose(t, createResp)

	indexResp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("index request failed: %v", err)
	}
	requireStatus(t, indexResp, http.StatusOK)
	indexBody := readBody(t, indexResp)
	if !strings.Contains(indexBody, bucket) {
		t.Fatalf("expected bucket %q to appear in index page", bucket)
	}

	gotoResp := postForm(t, client, srv.URL+"/bucket/goto", url.Values{
		"bucket": {bucket},
	})
	requireStatus(t, gotoResp, http.StatusSeeOther)
	discardAndClose(t, gotoResp)

	uploadResp := postMultipartUpload(t, client, srv.URL+"/object/upload/"+url.PathEscape(bucket), map[string]string{
		"bucket": bucket,
		"prefix": "integration/",
	}, []testUploadFile{
		{Filename: "a.txt", Contents: "alpha"},
		{Filename: "b.txt", Contents: "beta"},
	})
	requireStatus(t, uploadResp, http.StatusSeeOther)
	discardAndClose(t, uploadResp)

	browseURL := fmt.Sprintf("%s/bucket/%s?prefix=%s", srv.URL, url.PathEscape(bucket), url.QueryEscape("integration/"))
	browseResp, err := client.Get(browseURL)
	if err != nil {
		t.Fatalf("browse bucket request failed: %v", err)
	}
	requireStatus(t, browseResp, http.StatusOK)

	browseBody := readBody(t, browseResp)
	if !strings.Contains(browseBody, "integration/a.txt") {
		t.Fatalf("expected uploaded object key integration/a.txt in bucket page")
	}
	if !strings.Contains(browseBody, "integration/b.txt") {
		t.Fatalf("expected uploaded object key integration/b.txt in bucket page")
	}

	objectURL := fmt.Sprintf("%s/object/%s/%s", srv.URL, url.PathEscape(bucket), url.PathEscape("integration/a.txt"))
	objectResp, err := client.Get(objectURL)
	if err != nil {
		t.Fatalf("object details request failed: %v", err)
	}
	requireStatus(t, objectResp, http.StatusOK)
	objectBody := readBody(t, objectResp)
	if !strings.Contains(objectBody, "integration/a.txt") {
		t.Fatalf("expected object details page to include key integration/a.txt")
	}

	downloadURL := fmt.Sprintf("%s/download/%s/%s", srv.URL, url.PathEscape(bucket), url.PathEscape("integration/a.txt"))
	downloadResp, err := client.Get(downloadURL)
	if err != nil {
		t.Fatalf("download request failed: %v", err)
	}
	requireStatus(t, downloadResp, http.StatusOK)
	downloadBody := readBody(t, downloadResp)
	if !strings.Contains(downloadBody, "alpha") {
		t.Fatalf("expected downloaded object body to contain uploaded content")
	}

	deleteObjectResp := postForm(t, client, srv.URL+"/object/delete/"+url.PathEscape(bucket)+"/"+url.PathEscape("integration/a.txt"), url.Values{
		"bucket": {bucket},
		"key":    {"integration/a.txt"},
	})
	requireStatus(t, deleteObjectResp, http.StatusSeeOther)
	discardAndClose(t, deleteObjectResp)

	deleteNonEmptyBucketResp := postForm(t, client, srv.URL+"/bucket/delete", url.Values{
		"bucket": {bucket},
	})
	requireStatus(t, deleteNonEmptyBucketResp, http.StatusBadGateway)
	discardAndClose(t, deleteNonEmptyBucketResp)

	deleteObjectResp2 := postForm(t, client, srv.URL+"/object/delete/"+url.PathEscape(bucket)+"/"+url.PathEscape("integration/b.txt"), url.Values{
		"bucket": {bucket},
		"key":    {"integration/b.txt"},
	})
	requireStatus(t, deleteObjectResp2, http.StatusSeeOther)
	discardAndClose(t, deleteObjectResp2)

	deleteBucketResp := postForm(t, client, srv.URL+"/bucket/delete", url.Values{
		"bucket": {bucket},
	})
	requireStatus(t, deleteBucketResp, http.StatusSeeOther)
	discardAndClose(t, deleteBucketResp)

	logoutResp := postForm(t, client, srv.URL+"/logout", url.Values{})
	requireStatus(t, logoutResp, http.StatusSeeOther)
	requireLocation(t, logoutResp, "/login")
	discardAndClose(t, logoutResp)

	afterLogoutResp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("post-logout root request failed: %v", err)
	}
	requireStatus(t, afterLogoutResp, http.StatusSeeOther)
	requireLocation(t, afterLogoutResp, "/login")
	discardAndClose(t, afterLogoutResp)
}

func newIntegrationTestApp(endpoint string) *app {
	hashKey := []byte("0123456789abcdef0123456789abcdef")
	blockKey := []byte("abcdef0123456789abcdef0123456789")
	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(int(sessionTTL.Seconds()))

	return &app{
		tpl:            newTemplates(),
		region:         "us-east-1",
		endpoint:       endpoint,
		forcePathStyle: true,
		cookieName:     sessionCookieName,
		cookie:         sc,
	}
}

func newIntegrationTestMux(a *app) http.Handler {
	return newAppMux(a)
}

type testUploadFile struct {
	Filename string
	Contents string
}

func postMultipartUpload(t *testing.T, client *http.Client, endpoint string, fields map[string]string, files []testUploadFile) *http.Response {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write multipart field %q: %v", k, err)
		}
	}

	for _, f := range files {
		part, err := w.CreateFormFile("file", f.Filename)
		if err != nil {
			t.Fatalf("create multipart file part: %v", err)
		}
		if _, err := io.Copy(part, strings.NewReader(f.Contents)); err != nil {
			t.Fatalf("write multipart file content: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatalf("build multipart request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("execute multipart request: %v", err)
	}
	return resp
}

func postForm(t *testing.T, client *http.Client, endpoint string, values url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(endpoint, values)
	if err != nil {
		t.Fatalf("POST %s failed: %v", endpoint, err)
	}
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(t, resp)
		t.Fatalf("unexpected status: got=%d want=%d body=%q", resp.StatusCode, want, body)
	}
}

func requireLocation(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	got := resp.Header.Get("Location")
	if got != want {
		t.Fatalf("unexpected location header: got=%q want=%q", got, want)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(b)
}

func discardAndClose(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}
}
