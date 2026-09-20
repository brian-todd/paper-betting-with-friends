// Package fixtures resolves a captured HTTP response out of a recorded tree.
//
// Fixtures are recorded rather than authored. A hand-written body gets the
// shape wrong in the one direction that matters -- it fills in fields the real
// feed omits -- and the bugs worth catching here all live in that gap: a null
// startDate that unmarshals to year 1, a classification in a case nothing
// matches, a score that is present before the game is over.
//
// # Layout
//
// The request path becomes directories, the query becomes the leaf directory,
// and the instant of capture becomes the file name:
//
//	testdata/cfbd/teams/_/20260905T204909Z.json
//	testdata/cfbd/teams/ats/week=2&year=2026/20260905T204909Z.json
//	testdata/cfbd/scoreboard/classification=fbs/20260905T204909Z.json
//	                                           /20260905T205903Z.json
//
// "_" names an empty query. Nothing is ambiguous between a path segment and a
// query segment, because a query directory holds only files and a path
// directory holds only directories.
//
// The query is sorted before it is slugged, so a caller that assembles
// year-then-week and a capture taken as week-then-year still land in the same
// directory.
//
// # The file name is load-bearing
//
// It is the instant the response was recorded, and it is what a replaying test
// sets its clock to. A fixture of a live Saturday means nothing replayed
// against a real clock: every game in it kicked off in the past, so every
// status infers the same way and the thing under test never happens.
//
// # Failures are fixtures too
//
// A "502" infix serves that status with that body, and a ".bad" extension
// serves the bytes verbatim under a JSON content type. The 502 is the
// documented failure of CFBD's /games, and the malformed body has never been
// exercised against anything real.
package fixtures

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Providers, which are the first path element under testdata.
const (
	CFBD = "cfbd"
	CBBD = "cbbd"
)

// InstantLayout is the file-name form of a capture instant. It is
// scripts/capture-scoreboard.sh's `date -u +%Y%m%dT%H%M%SZ`, carried forward.
const InstantLayout = "20060102T150405Z"

// emptyQuery is the directory standing in for a request with no query string.
const emptyQuery = "_"

// The "all:" prefix is load-bearing. Without it //go:embed skips every path
// whose name begins with "_" or ".", which is precisely the directory standing
// in for an empty query -- so /venues and /teams, the two endpoints everything
// else holds a foreign key into, would be absent from the embedded tree while
// sitting on disk in plain sight.
//
//go:embed all:testdata
var embedded embed.FS

// A Capture is one recorded response.
type Capture struct {
	// Path is where it was read from, for error messages.
	Path string
	// At is the instant of capture, parsed from the file name.
	At time.Time
	// Status is the status to replay, 200 unless the name says otherwise.
	Status int
	// Malformed marks a body that is deliberately not JSON.
	Malformed bool
	// Body is the recorded bytes.
	Body []byte
}

// A Set is a tree of captures.
type Set struct {
	fsys fs.FS
	// name describes where the tree came from, for error messages.
	name string
}

// Embedded returns the fixtures compiled into the binary.
//
// Embedding rather than reading from disk is what lets `seed -fixtures` work
// from any directory, the same way the binary already carries its templates
// and migrations.
func Embedded() Set {
	sub, err := fs.Sub(embedded, "testdata")
	if err != nil {
		// Unreachable: the directory is embedded above.
		panic(fmt.Sprintf("fixtures: rooting embedded tree: %v", err))
	}
	return Set{fsys: sub, name: "embedded fixtures"}
}

// FromDir returns the fixtures under a directory, for the capture tool and for
// anything wanting to read a tree it has just written.
func FromDir(dir string) Set {
	return Set{fsys: os.DirFS(dir), name: dir}
}

// Dir is the directory a request resolves to, relative to the tree root.
//
// The result is checked against fs.ValidPath rather than trusted. io/fs would
// reject a climbing path anyway, so this is not the only thing standing
// between a request path and the filesystem -- but "some other layer rejects
// it" is a property that holds until someone swaps the layer, and the fixture
// server hands it a path straight off the wire.
func Dir(provider, requestPath, rawQuery string) (string, error) {
	slug, err := QuerySlug(rawQuery)
	if err != nil {
		return "", err
	}

	dir := path.Join(provider, strings.Trim(requestPath, "/"), slug)

	// Two separate escapes, and fs.ValidPath only catches the first. A path
	// with enough ".." leaves the tree ("../etc/passwd"), which io/fs would
	// reject anyway; one with exactly enough stays valid and merely leaves the
	// *provider* directory, so "/.." resolves to "_" and a cfbd server would
	// answer out of the cbbd tree. Require both.
	if !fs.ValidPath(dir) || (dir != provider && !strings.HasPrefix(dir, provider+"/")) {
		return "", fmt.Errorf("request path %q does not resolve inside %s/", requestPath, provider)
	}
	return dir, nil
}

// QuerySlug is the leaf directory name for a raw query string.
//
// Exported because the capture tool has to agree with the server about it, and
// two implementations of this would disagree eventually.
func QuerySlug(rawQuery string) (string, error) {
	if rawQuery == "" {
		return emptyQuery, nil
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("parsing query %q: %w", rawQuery, err)
	}

	var parts []string
	for _, key := range slices.Sorted(maps.Keys(values)) {
		vs := slices.Clone(values[key])
		slices.Sort(vs)
		for _, v := range vs {
			parts = append(parts, escape(key)+"="+escape(v))
		}
	}
	return strings.Join(parts, "&"), nil
}

// Sequence returns every capture recorded for a request, oldest first.
//
// It is a sequence rather than a body because the convergence tests need the
// same endpoint to answer differently on the second call than on the first --
// two writers seeing one game at two moments is the whole subject.
func (s Set) Sequence(provider, requestPath, rawQuery string) ([]Capture, error) {
	dir, err := Dir(provider, requestPath, rawQuery)
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(s.fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("no fixtures at %s in %s: %w", dir, s.name, err)
	}

	var captures []Capture
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		capture, err := parseName(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, entry.Name(), err)
		}
		capture.Path = path.Join(dir, entry.Name())
		capture.Body, err = fs.ReadFile(s.fsys, capture.Path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", capture.Path, err)
		}
		captures = append(captures, capture)
	}

	if len(captures) == 0 {
		return nil, fmt.Errorf("no fixture files in %s in %s", dir, s.name)
	}

	slices.SortFunc(captures, func(a, b Capture) int { return a.At.Compare(b.At) })
	return captures, nil
}

// parseName reads a capture's instant, status and body kind out of its file
// name: <instant>[.<status>].json, or <instant>[.<status>].bad.
func parseName(name string) (Capture, error) {
	capture := Capture{Status: 200}

	stem, ok := strings.CutSuffix(name, ".json")
	if !ok {
		stem, ok = strings.CutSuffix(name, ".bad")
		if !ok {
			return Capture{}, fmt.Errorf("name does not end in .json or .bad")
		}
		capture.Malformed = true
	}

	if instant, suffix, found := strings.Cut(stem, "."); found {
		status, err := strconv.Atoi(suffix)
		if err != nil {
			return Capture{}, fmt.Errorf("%q is not an HTTP status", suffix)
		}
		capture.Status = status
		stem = instant
	}

	at, err := time.Parse(InstantLayout, stem)
	if err != nil {
		return Capture{}, fmt.Errorf("%q is not a capture instant (%s): %w", stem, InstantLayout, err)
	}
	capture.At = at

	return capture, nil
}

// escape makes a query key or value safe as a path element. The joiners "="
// and "&" are applied by the caller and stay literal, so a directory name
// still reads as the query it came from.
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '.', c == '_', c == '-', c == '~', c == '+':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
