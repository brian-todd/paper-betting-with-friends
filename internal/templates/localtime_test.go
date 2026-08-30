package templates

import (
	assets "github.com/brian/paper-betting-with-friends"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// newTestRenderer builds a renderer over the real templates in the given zone.
func newTestRenderer(t *testing.T, loc *time.Location) *Renderer {
	t.Helper()

	r, err := NewRenderer(assets.FS, false, loc)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	return r
}

// localTime is only reachable through a parsed template, so exercise it the way
// pages do rather than reaching for the func value directly.
func renderLocalTime(t *testing.T, r *Renderer, ts time.Time, format string) string {
	t.Helper()

	tmpl, err := r.templates["login"].Clone()
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if _, err := tmpl.Parse(`{{define "probe"}}{{localTime .Time .Format}}{{end}}`); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var sb strings.Builder
	data := map[string]any{"Time": ts, "Format": format}
	if err := tmpl.ExecuteTemplate(&sb, "probe", data); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	return sb.String()
}

func TestLocalTimeEmitsMachineReadableUTC(t *testing.T) {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	// The 9 PM Eastern game that used to render as 1 AM the next day.
	kickoff := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		format       string
		wantFallback string
	}{
		{"datetime", "datetime", "Fri, Aug 28 • 9:00 PM EDT"},
		{"time", "time", "9:00 PM EDT"},
		{"date", "date", "Aug 28"},
		{"mediumdate", "mediumdate", "Aug 28, 2026"},
		{"fulldate", "fulldate", "August 28, 2026"},
		{"longdate", "longdate", "Friday, August 28, 2026"},
		{"shortdatetime", "shortdatetime", "8/28 9:00 PM"},
		{"mediumdatetime", "mediumdatetime", "Aug 28, 2026 9:00 PM"},
	}

	r := newTestRenderer(t, eastern)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderLocalTime(t, r, kickoff, tt.format)

			// The datetime attribute is what the browser re-renders from, so it
			// has to stay an unambiguous UTC instant regardless of the format.
			if want := `datetime="2026-08-29T01:00:00Z"`; !strings.Contains(got, want) {
				t.Errorf("output %q missing %s", got, want)
			}
			if want := `data-format="` + tt.format + `"`; !strings.Contains(got, want) {
				t.Errorf("output %q missing %s", got, want)
			}
			// The fallback text is what a reader without JavaScript sees, so it
			// must already be in the app's zone rather than UTC.
			if !strings.Contains(got, ">"+tt.wantFallback+"<") {
				t.Errorf("output %q missing fallback text %q", got, tt.wantFallback)
			}
		})
	}
}

func TestLocalTimeRendersFallbackInConfiguredZone(t *testing.T) {
	kickoff := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		zone string
		want string
	}{
		{"eastern", "America/New_York", "Aug 28"},
		{"pacific", "America/Los_Angeles", "Aug 28"},
		{"utc", "UTC", "Aug 29"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatalf("LoadLocation() error = %v", err)
			}

			got := renderLocalTime(t, newTestRenderer(t, loc), kickoff, "date")
			if !strings.Contains(got, ">"+tt.want+"<") {
				t.Errorf("output %q, want fallback %q for %s", got, tt.want, tt.zone)
			}
		})
	}
}

func TestLocalTimeEdgeCases(t *testing.T) {
	r := newTestRenderer(t, time.UTC)

	t.Run("zero time renders nothing", func(t *testing.T) {
		if got := renderLocalTime(t, r, time.Time{}, "datetime"); got != "" {
			t.Errorf("output = %q, want empty", got)
		}
	})

	t.Run("unknown format falls back to datetime", func(t *testing.T) {
		got := renderLocalTime(t, r, time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC), "nonsense")
		if !strings.Contains(got, `data-format="datetime"`) {
			t.Errorf("output = %q, want it to fall back to datetime", got)
		}
	})

	t.Run("nil location means UTC", func(t *testing.T) {
		got := renderLocalTime(t, newTestRenderer(t, nil), time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC), "date")
		if !strings.Contains(got, ">Aug 29<") {
			t.Errorf("output = %q, want the UTC day", got)
		}
	})
}

// Every format the templates ask for has to exist, or localTime silently
// downgrades it to "datetime" at runtime.
func TestPageTemplatesOnlyUseKnownTimeFormats(t *testing.T) {
	callPattern := regexp.MustCompile(`localTime\s+\S+\s+"([^"]+)"`)

	pages, err := filepath.Glob("../../templates/pages/*.html")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("found no page templates")
	}

	var seen int
	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("reading %s: %v", page, err)
		}

		for _, match := range callPattern.FindAllStringSubmatch(string(body), -1) {
			seen++
			if _, ok := timeLayouts[match[1]]; !ok {
				t.Errorf("%s uses unknown localTime format %q", filepath.Base(page), match[1])
			}
		}
	}

	// Guard the guard: a broken pattern would make this test vacuously pass.
	if seen == 0 {
		t.Error("matched no localTime calls; the pattern is probably wrong")
	}
}

// The footer lives in the layout, not a page, so the format check above would
// not have covered it.
func TestLayoutTemplatesOnlyUseKnownTimeFormats(t *testing.T) {
	callPattern := regexp.MustCompile(`localTime\s+\S+\s+"([^"]+)"`)

	layouts, err := filepath.Glob("../../templates/layouts/*.html")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(layouts) == 0 {
		t.Fatal("found no layout templates")
	}

	for _, layout := range layouts {
		body, err := os.ReadFile(layout)
		if err != nil {
			t.Fatalf("reading %s: %v", layout, err)
		}

		for _, match := range callPattern.FindAllStringSubmatch(string(body), -1) {
			if _, ok := timeLayouts[match[1]]; !ok {
				t.Errorf("%s uses unknown localTime format %q", filepath.Base(layout), match[1])
			}
		}
	}
}

// Globals let the layout reach data no handler passes it. A page's own keys
// have to win, or a global could silently shadow real page data.
func TestRendererGlobals(t *testing.T) {
	renderer := &Renderer{}

	t.Run("no globals leaves data untouched", func(t *testing.T) {
		data := map[string]any{"Title": "Games"}
		got, ok := renderer.withGlobals(data).(map[string]any)
		if !ok || got["Title"] != "Games" || len(got) != 1 {
			t.Fatalf("withGlobals = %v, want the original map", got)
		}
	})

	renderer.SetGlobals(func() map[string]any {
		return map[string]any{"SyncStatus": "fresh", "Title": "from globals"}
	})

	t.Run("globals are merged underneath page data", func(t *testing.T) {
		got, ok := renderer.withGlobals(map[string]any{"Title": "Games"}).(map[string]any)
		if !ok {
			t.Fatalf("withGlobals returned %T, want map[string]any", got)
		}
		if got["SyncStatus"] != "fresh" {
			t.Errorf("SyncStatus = %v, want fresh", got["SyncStatus"])
		}
		if got["Title"] != "Games" {
			t.Errorf("Title = %v, want the page's own value to win", got["Title"])
		}
	})

	t.Run("the page's map is not mutated", func(t *testing.T) {
		data := map[string]any{"Title": "Games"}
		renderer.withGlobals(data)
		if _, present := data["SyncStatus"]; present {
			t.Error("withGlobals wrote back into the caller's map")
		}
	})

	t.Run("non-map data passes through", func(t *testing.T) {
		type pageData struct{ Title string }
		data := pageData{Title: "Bets"}
		if got := renderer.withGlobals(data); got != any(data) {
			t.Errorf("withGlobals = %v, want the struct unchanged", got)
		}
	})
}
