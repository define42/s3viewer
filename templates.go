package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

//go:embed templates.html
var htmlTemplatesFS embed.FS

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
		"hasErr": func(v any) bool {
			s, ok := v.(string)
			if !ok {
				return false
			}
			return strings.TrimSpace(s) != ""
		},
	}
	return template.Must(template.New("base").Funcs(funcs).ParseFS(htmlTemplatesFS, "templates.html"))
}

func (a *app) render(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data["Now"] = time.Now().Format(time.RFC3339)
	if _, ok := data["IsAuthenticated"]; !ok {
		data["IsAuthenticated"] = false
	}
	if err := a.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("template execution failed", "template", name, "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *app) renderError(w http.ResponseWriter, msg string, err error, code int) {
	slog.Error(msg, "error", err, "status_code", code)
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
