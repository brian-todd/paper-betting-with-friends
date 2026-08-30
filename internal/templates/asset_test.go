package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
)

// wantVersion is the URL asset should produce for the given contents.
func wantVersion(path string, content []byte) string {
	sum := sha256.Sum256(content)
	return path + "?v=" + hex.EncodeToString(sum[:])[:assetVersionLength]
}

func TestAsset(t *testing.T) {
	const css = "/static/css/style.css"
	body := []byte("body{}")

	newFS := func() fstest.MapFS {
		return fstest.MapFS{
			"static/css/style.css": &fstest.MapFile{Data: body},
		}
	}

	t.Run("versions an existing file by content", func(t *testing.T) {
		r := &Renderer{files: newFS()}
		if got, want := r.asset(css), wantVersion(css, body); got != want {
			t.Errorf("asset() = %q, want %q", got, want)
		}
	})

	t.Run("missing file stays unversioned", func(t *testing.T) {
		r := &Renderer{files: newFS()}
		if got := r.asset("/static/js/nope.js"); got != "/static/js/nope.js" {
			t.Errorf("asset() = %q, want unversioned path", got)
		}
	})

	t.Run("production caches the first version", func(t *testing.T) {
		files := newFS()
		r := &Renderer{files: files}

		first := r.asset(css)
		files["static/css/style.css"] = &fstest.MapFile{Data: []byte("body{color:red}")}

		if got := r.asset(css); got != first {
			t.Errorf("asset() = %q after a change, want the cached %q", got, first)
		}
	})

	t.Run("dev mode re-reads on every call", func(t *testing.T) {
		files := newFS()
		r := &Renderer{files: files, devMode: true}

		first := r.asset(css)
		files["static/css/style.css"] = &fstest.MapFile{Data: []byte("body{color:red}")}

		if got := r.asset(css); got == first {
			t.Errorf("asset() = %q after a change, want a new version", got)
		}
	})

	// The reason the version is a content hash rather than a modification time:
	// every file in an embed.FS reports the zero time, so an mtime-based version
	// would be identical for every asset and would never change across deploys,
	// pinning stale files in browser caches. fstest.MapFS has the same property.
	t.Run("distinguishes files that share a modification time", func(t *testing.T) {
		files := fstest.MapFS{
			"static/css/a.css": &fstest.MapFile{Data: []byte("a{}")},
			"static/css/b.css": &fstest.MapFile{Data: []byte("b{}")},
		}
		r := &Renderer{files: files}

		a := r.asset("/static/css/a.css")
		b := r.asset("/static/css/b.css")

		if _, aVersion, _ := strings.Cut(a, "?v="); aVersion == "" {
			t.Fatalf("asset() = %q, want a version", a)
		}
		if strings.TrimPrefix(a, "/static/css/a.css") == strings.TrimPrefix(b, "/static/css/b.css") {
			t.Errorf("different files got the same version: %q and %q", a, b)
		}
	})
}
