package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// htmx compiles a handful of attribute values with `new Function`, which a
// Content-Security-Policy blocks unless it grants 'unsafe-eval'. The policy in
// cmd/server/middleware.go deliberately does not, so any of these attributes
// would stop working in a browser -- silently, with no server-side error and
// nothing an HTTP-level test would notice. Bind the listener in a <script>
// block instead; templates/pages/game_detail.html has the pattern.
func TestTemplatesAvoidHtmxFeaturesNeedingUnsafeEval(t *testing.T) {
	forbidden := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"hx-on", regexp.MustCompile(`hx-on[:=]`)},
		{"hx-vals js:", regexp.MustCompile(`hx-vals\s*=\s*"\s*js:`)},
		{"hx-headers js:", regexp.MustCompile(`hx-headers\s*=\s*"\s*js:`)},
		// hx-trigger event filters, e.g. hx-trigger="click[ctrlKey]".
		{"hx-trigger event filter", regexp.MustCompile(`hx-trigger\s*=\s*"[^"]*\[`)},
	}

	var templateFiles []string
	for _, dir := range []string{"pages", "layouts", "partials"} {
		found, err := filepath.Glob(filepath.Join("../../templates", dir, "*.html"))
		if err != nil {
			t.Fatalf("Glob(%s) error = %v", dir, err)
		}
		templateFiles = append(templateFiles, found...)
	}
	if len(templateFiles) == 0 {
		t.Fatal("found no templates; the glob is probably wrong")
	}

	for _, file := range templateFiles {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		for _, f := range forbidden {
			if f.pattern.Match(body) {
				t.Errorf("%s uses %s, which htmx evaluates with new Function and the CSP blocks",
					filepath.Base(file), f.name)
			}
		}
	}
}
