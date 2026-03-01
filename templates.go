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
	if _, ok := data["IsAuthenticated"]; !ok {
		data["IsAuthenticated"] = false
	}
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
    :root {
      color-scheme: dark;
      --bg: #0f1117;
      --surface: #161b22;
      --surface-alt: #1d2531;
      --text: #e6edf3;
      --muted: #98a3b3;
      --border: #2b3545;
      --link: #7cc4ff;
      --code-bg: #202938;
      --btn-bg: #1d2533;
      --btn-bg-hover: #263246;
      --warn-border: #7a6231;
      --warn-bg: #332912;
      --danger: #ef6b6b;
    }
    body {
      font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
      margin: 24px;
      background: var(--bg);
      color: var(--text);
    }
    .topbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; }
    .topbar-right { display: flex; align-items: center; gap: 12px; }
    a { text-decoration: none; color: var(--link); }
    a:hover { text-decoration: underline; }
    .muted { color: var(--muted); }
    .row { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
    .card { border: 1px solid var(--border); border-radius: 10px; padding: 14px; margin: 12px 0; background: var(--surface); }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid var(--border); padding: 8px; text-align: left; vertical-align: top; }
    th { background: var(--surface-alt); }
    code { background: var(--code-bg); color: var(--text); padding: 2px 4px; border-radius: 4px; }
    .btn { display:inline-block; padding: 8px 10px; border:1px solid var(--border); border-radius: 8px; background: var(--btn-bg); color: var(--text); cursor: pointer;}
    .btn:hover { background: var(--btn-bg-hover); text-decoration: none; }
    input[type="text"]{ padding:8px; border:1px solid var(--border); border-radius: 8px; min-width: 280px; background: var(--surface-alt); color: var(--text);}
    input[type="password"]{ padding:8px; border:1px solid var(--border); border-radius: 8px; min-width: 280px; background: var(--surface-alt); color: var(--text);}
    input[type="file"]{ padding:6px; color: var(--text); }
    .danger { border-color: var(--danger); color: #ffd1d1; }
    .warn { border: 1px solid var(--warn-border); background: var(--warn-bg); padding: 10px; border-radius: 10px; }
    hr { border: 0; border-top: 1px solid var(--border); }
  </style>
</head>
<body>
  <div class="topbar">
    <div class="row">
      <h2 style="margin:0;"><a href="/">S3 Viewer</a></h2>
    </div>
    <div class="topbar-right">
      <span class="muted">Generated: {{.Now}}</span>
      {{if .IsAuthenticated}}
      <form method="post" action="/logout" style="margin:0;">
        <button class="btn" type="submit">Logout</button>
      </form>
      {{end}}
    </div>
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

{{define "login"}}
{{template "layout-start" .}}
  <div class="card" style="max-width:480px;">
    <h3 style="margin-top:0;">Login</h3>
    <form method="post" action="/login">
      <div class="row">
        <input type="text" name="access_key" value="{{.AccessKey}}" placeholder="Access Key" required />
      </div>
      <div class="row" style="margin-top:10px;">
        <input type="password" name="secret_key" placeholder="Secret Key" required />
      </div>
      {{if hasErr .LoginError}}
        <p class="warn">
          <strong>Login failed</strong><br/>
          <span class="muted">{{.LoginError}}</span>
        </p>
      {{end}}
      <div class="row" style="margin-top:12px;">
        <button class="btn" type="submit">Login</button>
      </div>
    </form>
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
        <input type="text" name="key" placeholder="optional: override key (single file only)" />
        <input type="file" name="file" multiple required />
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
      <thead><tr><th>Key</th><th>Size</th><th>Last modified</th><th>Metadata</th><th>Tags</th><th></th></tr></thead>
      <tbody>
        {{range .Objects}}
          <tr>
            <td><a href="{{.DetailsURL}}"><code>{{.Key}}</code></a></td>
            <td>{{.Size}}</td>
            <td class="muted">{{.LastModified}}</td>
            <td>
              {{if hasErr .MetadataError}}
                <span class="muted">{{.MetadataError}}</span>
              {{else if not .Metadata}}
                <span class="muted">None</span>
              {{else}}
                {{range .Metadata}}
                  <div><code>{{.K}}</code>=<code>{{.V}}</code></div>
                {{end}}
              {{end}}
            </td>
            <td>
              {{if hasErr .TagError}}
                <span class="muted">{{.TagError}}</span>
              {{else if not .Tags}}
                <span class="muted">None</span>
              {{else}}
                {{range .Tags}}
                  <div><code>{{.K}}</code>=<code>{{.V}}</code></div>
                {{end}}
              {{end}}
            </td>
            <td><a class="btn" href="{{.DownloadURL}}">Download</a></td>
          </tr>
        {{end}}
      </tbody>
    </table>

    <div class="row" style="margin-top:12px;">
      {{if .HasPrev}}
        <a class="btn" href="{{.PrevPageURL}}">← Prev page</a>
      {{end}}
      {{if .HasNext}}
        <a class="btn" href="{{.NextPageURL}}">Next page →</a>
      {{end}}
    </div>
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
