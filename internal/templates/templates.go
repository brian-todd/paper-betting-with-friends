package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// timeLayouts maps the format names accepted by the localTime template
// function to the Go layout used for the server-rendered fallback text.
//
// Keep these keys in sync with FORMATS in static/js/localtime.js, which holds
// the matching Intl.DateTimeFormat options. A name present here but missing
// there leaves the fallback on screen unconverted.
var timeLayouts = map[string]string{
	"datetime":       "Mon, Jan 2 • 3:04 PM MST",
	"time":           "3:04 PM MST",
	"date":           "Jan 2",
	"mediumdate":     "Jan 2, 2006",
	"fulldate":       "January 2, 2006",
	"longdate":       "Monday, January 2, 2006",
	"shortdatetime":  "1/2 3:04 PM",
	"mediumdatetime": "Jan 2, 2006 3:04 PM",

	// relative is rendered by the browser as "4 minutes ago" or "in 11
	// minutes"; the layout here is only what a reader without JavaScript sees.
	"relative": "3:04 PM MST",
}

// Directories inside the renderer's filesystem. They are fixed rather than
// configurable because the same names have to hold for the embedded tree and
// the on-disk one for the two to be interchangeable.
const (
	templateDir = "templates"
	staticDir   = "static"
)

// Renderer handles HTML template rendering.
type Renderer struct {
	templates map[string]*template.Template
	mu        sync.RWMutex

	// files supplies both trees. In production it is the embedded copy; in
	// development it is the working directory, which is what makes template
	// edits take effect without a restart.
	files fs.FS

	devMode  bool
	location *time.Location

	assetVersions sync.Map // URL path -> version string

	// globalsMu guards globals. It is deliberately not mu: Render already holds
	// a read lock on that while it looks up the template, and taking the same
	// RWMutex twice deadlocks if a writer queues between the two.
	globalsMu sync.RWMutex
	globals   func() map[string]any
}

// SetGlobals registers a function supplying values merged into the data of
// every full-page render. It exists for things the base layout needs on every
// page — the sync status in the footer — which no handler should have to
// remember to pass, and which would otherwise have to be threaded through all
// of them.
//
// Handler keys win on collision, so a page can always override a global. Data
// that is not a map[string]any is passed through untouched.
func (r *Renderer) SetGlobals(fn func() map[string]any) {
	r.globalsMu.Lock()
	defer r.globalsMu.Unlock()
	r.globals = fn
}

// withGlobals merges the registered globals underneath the handler's own data.
func (r *Renderer) withGlobals(data any) any {
	r.globalsMu.RLock()
	globals := r.globals
	r.globalsMu.RUnlock()

	if globals == nil {
		return data
	}

	page, ok := data.(map[string]any)
	if !ok {
		return data
	}

	merged := make(map[string]any, len(page)+2)
	maps.Copy(merged, globals())
	maps.Copy(merged, page)
	return merged
}

// NewRenderer creates a new template renderer reading from files, which must
// contain the templates/ and static/ trees. In development mode, templates are
// reloaded on each request.
//
// loc is the timezone used for server-rendered timestamps. It is only a
// fallback: localTime also emits a machine-readable value that the browser
// re-renders in the reader's own zone. A nil loc means UTC.
func NewRenderer(files fs.FS, devMode bool, loc *time.Location) (*Renderer, error) {
	if loc == nil {
		loc = time.UTC
	}

	r := &Renderer{
		templates: make(map[string]*template.Template),
		files:     files,
		devMode:   devMode,
		location:  loc,
	}

	if err := r.loadTemplates(); err != nil {
		return nil, err
	}

	return r, nil
}

// loadTemplates loads all templates from the base directory.
func (r *Renderer) loadTemplates() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	layoutPath := path.Join(templateDir, "layouts", "base.html")

	// Load page templates.
	pagesDir := path.Join(templateDir, "pages")
	pages, err := fs.Glob(r.files, path.Join(pagesDir, "*.html"))
	if err != nil {
		return fmt.Errorf("failed to glob page templates: %w", err)
	}

	// fs.Glob reports a directory that does not exist as no matches rather than
	// an error, so without this the renderer builds successfully with nothing in
	// it and the failure only surfaces as a 500 on every page. The way that
	// actually happens in production is ENV being unset: main then passes
	// os.DirFS(".") instead of the embedded assets, and the image contains
	// nothing but the binary.
	if len(pages) == 0 {
		return fmt.Errorf("found no page templates in %s; if this is a deployment, "+
			"check that ENV is set to production so the embedded assets are used", pagesDir)
	}

	// Template functions available in all templates.
	funcMap := template.FuncMap{
		"currentYear": func() int {
			return time.Now().In(r.location).Year()
		},
		// localTime renders an instant as a <time> element carrying both a
		// machine-readable UTC value and the format name, so
		// static/js/localtime.js can rewrite it in the reader's timezone. The
		// text content is the same instant in the app's configured zone, which
		// is what a reader without JavaScript sees.
		"localTime": func(t time.Time, format string) template.HTML {
			if t.IsZero() {
				return ""
			}

			layout, ok := timeLayouts[format]
			if !ok {
				// An unknown name is a template typo, not user input. Fall back
				// to the most common format rather than rendering nothing.
				slog.Warn("unknown localTime format", "format", format)
				format, layout = "datetime", timeLayouts["datetime"]
			}

			return template.HTML(fmt.Sprintf(
				`<time datetime="%s" data-format="%s">%s</time>`,
				t.UTC().Format(time.RFC3339),
				format,
				template.HTMLEscapeString(t.In(r.location).Format(layout)),
			))
		},
		// deref dereferences a pointer to int.
		"deref": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
		// derefStr dereferences a pointer to string.
		"derefStr": func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		},
		// uuidToString converts a *uuid.UUID to string for comparison.
		"uuidToString": func(p *uuid.UUID) string {
			if p == nil {
				return ""
			}
			return p.String()
		},
		"asset": r.asset,
	}

	for _, page := range pages {
		name := path.Base(page)
		name = name[:len(name)-5] // Remove .html extension.

		tmpl, err := template.New("").Funcs(funcMap).ParseFS(r.files, layoutPath, page)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		r.templates[name] = tmpl
		slog.Debug("loaded template", "template", name)
	}

	return nil
}

// assetVersionLength is how much of the content hash goes in the URL. Eight
// hex characters is ample to tell two builds of one file apart, and keeps the
// markup readable.
const assetVersionLength = 8

// asset appends a cache-busting version to a /static/ URL path, derived from a
// hash of the file's contents.
//
// The static file server sends only Last-Modified, no Cache-Control, so
// browsers apply heuristic caching: a file that had not changed for months may
// be reused from cache for days without ever revalidating. Changing the URL
// whenever the file changes defeats that. A missing file renders its path
// unversioned rather than failing the page.
//
// The version is the content rather than the modification time because an
// embedded file has no meaningful mtime -- everything in an embed.FS reports
// the zero time, which would collapse every asset onto one version and pin
// stale files in caches across deploys.
func (r *Renderer) asset(urlPath string) string {
	if v, ok := r.assetVersions.Load(urlPath); ok && !r.devMode {
		if v == "" {
			return urlPath
		}
		return urlPath + "?v=" + v.(string)
	}

	fsPath := path.Join(staticDir, strings.TrimPrefix(urlPath, "/static/"))
	var version string
	if content, err := fs.ReadFile(r.files, fsPath); err == nil {
		sum := sha256.Sum256(content)
		version = hex.EncodeToString(sum[:])[:assetVersionLength]
	} else {
		slog.Warn("asset not found, serving unversioned url", "path", urlPath, "error", err)
	}
	r.assetVersions.Store(urlPath, version)

	if version == "" {
		return urlPath
	}
	return urlPath + "?v=" + version
}

// Render renders a template with the given data.
func (r *Renderer) Render(w io.Writer, name string, data any) error {
	// In development mode, reload templates on each request.
	if r.devMode {
		if err := r.loadTemplates(); err != nil {
			slog.Error("failed to reload templates", "error", err)
		}
	}

	r.mu.RLock()
	tmpl, ok := r.templates[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("template %s not found", name)
	}

	return tmpl.ExecuteTemplate(w, "base", r.withGlobals(data))
}

// RenderPartial renders a partial template (without the base layout).
func (r *Renderer) RenderPartial(w io.Writer, name string, data any) error {
	partialPath := path.Join(templateDir, "partials", name+".html")

	tmpl, err := template.ParseFS(r.files, partialPath)
	if err != nil {
		return fmt.Errorf("failed to parse partial %s: %w", name, err)
	}

	return tmpl.Execute(w, data)
}
