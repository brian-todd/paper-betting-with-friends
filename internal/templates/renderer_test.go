package templates

import (
	"strings"
	"testing"
	"testing/fstest"
)

// fs.Glob reports a missing directory as no matches rather than an error, so a
// renderer built over the wrong filesystem used to succeed with nothing loaded
// and only fail later, as a 500 on every page with the boot log looking clean.
//
// The way this happens for real is ENV being unset in a deployment: main passes
// os.DirFS(".") instead of the embedded assets, and the production image holds
// nothing but the binary.
func TestNewRendererRejectsAFilesystemWithNoTemplates(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
	}{
		{
			name:  "entirely empty, as in the production image",
			files: fstest.MapFS{},
		},
		{
			// Everything is present except the pages themselves.
			name: "layout but no pages",
			files: fstest.MapFS{
				"templates/layouts/base.html": &fstest.MapFile{Data: []byte(`{{define "base"}}{{end}}`)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRenderer(tt.files, false, nil)
			if err == nil {
				t.Fatal("NewRenderer() = nil error, want a refusal to build an empty renderer")
			}
			// The message has to name the likely cause: the symptom otherwise
			// points at templates, which is not where the mistake was made.
			if !strings.Contains(err.Error(), "ENV") {
				t.Errorf("error = %q, want it to mention ENV as the likely cause", err)
			}
		})
	}
}

// The real asset filesystem must still load, or the guard above is just a way
// to break the server.
func TestNewRendererLoadsTheEmbeddedTemplates(t *testing.T) {
	r := newTestRenderer(t, nil)

	if len(r.templates) == 0 {
		t.Fatal("no templates loaded from the embedded assets")
	}
	if _, ok := r.templates["login"]; !ok {
		t.Error("login page did not load")
	}
}
