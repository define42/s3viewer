package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

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
    <h4 style="margin-top:0;">Upload files</h4>
    <form method="post" action="{{.UploadAction}}" enctype="multipart/form-data">
      <input type="hidden" name="bucket" value="{{.Bucket}}" />
      <input type="hidden" name="prefix" value="{{.Prefix}}" />
      <div class="row">
        <input type="file" name="file" multiple required />
        <input type="text" name="key" placeholder="optional: override key (single file only)" />
        <button class="btn" type="submit">Upload</button>
      </div>
      <p class="muted" style="margin-bottom:0;">Select one or more files. Uploads into current prefix unless you override key.</p>
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
