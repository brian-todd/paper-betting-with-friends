// Command capture records live API responses into the fixture tree.
//
// Every run spends metered requests -- CFBD allows 30,000 a month and the
// background jobs are most of it -- so this is a deliberate act, not something
// a test or a build does.
//
// Usage:
//
//	go run ./cmd/capture -provider cfbd /venues /teams '/games?year=2026&week=2'
//
// It is Go rather than shell for one reason: the directory a capture is
// written to has to be the directory the fixture server looks in, and two
// implementations of that rule would eventually disagree. `scripts/capture.sh`
// is a wrapper that loads .env and calls this.
//
// A failed request is captured too. A 502 from /games at 3am is data -- it is
// the documented failure this system has to survive -- so it lands under a
// ".502." infix rather than being discarded, and a body that is not JSON lands
// under ".bad".
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

// hosts are the real upstreams, by provider.
var hosts = map[string]string{
	fixtures.CFBD: "https://api.collegefootballdata.com",
	fixtures.CBBD: "https://api.collegebasketballdata.com",
}

// keyVars name the environment variable holding each provider's API key.
var keyVars = map[string]string{
	fixtures.CFBD: "CFB_DATA_API_KEY",
	fixtures.CBBD: "CBB_DATA_API_KEY",
}

func main() {
	provider := flag.String("provider", fixtures.CFBD, "which API to capture: cfbd or cbbd")
	out := flag.String("out", filepath.Join("internal", "fixtures", "testdata"), "fixture tree to write into")
	timeout := flag.Duration("timeout", time.Minute, "per-request timeout")
	appendTo := flag.Bool("append", false,
		"Add to a route's existing captures instead of replacing them, building a sequence the fixture server replays in order.")
	replace := flag.Bool("replace", false,
		"Discard a route's existing captures even if there are several. Without this, overwriting a sequence is refused.")
	flag.Parse()

	logging.Setup("development")

	if *appendTo && *replace {
		slog.Error("-append and -replace ask for opposite things")
		os.Exit(1)
	}

	mode := modeReplaceOne
	switch {
	case *appendTo:
		mode = modeAppend
	case *replace:
		mode = modeReplaceAll
	}

	if err := run(*provider, *out, *timeout, mode, flag.Args()); err != nil {
		slog.Error("capture failed", "error", err)
		os.Exit(1)
	}
}

// How a capture treats what is already on the route.
//
// The default replaces a single existing capture and refuses a series, which
// is the behaviour the alternatives do not have. Pure append is how the
// scoreboard series was built, and it is wrong for everything else: the server
// replays oldest first, so a second capture of /venues leaves the seed reading
// the stale one forever and the fresh one never reached at all -- a re-capture
// that appears to do nothing. Pure replace is right for refreshing a route,
// and would quietly throw away twenty-six recorded snapshots if it were the
// default.
type writeMode int

const (
	modeReplaceOne writeMode = iota
	modeAppend
	modeReplaceAll
)

func run(provider, out string, timeout time.Duration, mode writeMode, paths []string) error {
	host, ok := hosts[provider]
	if !ok {
		return fmt.Errorf("unknown provider %q, want cfbd or cbbd", provider)
	}
	if len(paths) == 0 {
		return errors.New("nothing to capture; pass one or more paths, e.g. /venues '/games?year=2026&week=2'")
	}

	apiKey := os.Getenv(keyVars[provider])
	if apiKey == "" {
		return fmt.Errorf("%s is not set; capture spends real requests and cannot run without it", keyVars[provider])
	}

	// One instant for the whole run, so a set of endpoints captured together
	// share a file name and can be recognised as one snapshot of the feed.
	// scripts/capture-scoreboard.sh did the same across classifications.
	instant := time.Now().UTC().Format(fixtures.InstantLayout)
	client := &http.Client{Timeout: timeout}

	var failed int
	for _, raw := range paths {
		if err := capture(client, provider, host, apiKey, out, instant, mode, raw); err != nil {
			// Keep going: a run capturing eight endpoints should not lose the
			// seven that worked because the eighth 502'd.
			slog.Error("capturing", "path", raw, "error", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d captures failed", failed, len(paths))
	}
	return nil
}

func capture(client *http.Client, provider, host, apiKey, out, instant string, mode writeMode, raw string) error {
	requestPath, rawQuery, _ := strings.Cut(strings.TrimPrefix(raw, host), "?")

	dir, err := fixtures.Dir(provider, requestPath, rawQuery)
	if err != nil {
		return err
	}

	full := filepath.Join(out, filepath.FromSlash(dir))
	if err := os.MkdirAll(full, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", full, err)
	}
	// Before the request, not after: refusing to overwrite a sequence is not
	// worth a metered call to discover.
	existing, err := routeCaptures(full)
	if err != nil {
		return err
	}
	// A capture is identified by its instant, and the instant is one second
	// wide, so a second append inside the same second would land on the same
	// file name and overwrite what it meant to extend -- an append that
	// silently does nothing.
	if mode == modeAppend && slices.Contains(existing, instant+".json") {
		return fmt.Errorf("%s already holds a capture at %s: captures are named by the second "+
			"they were taken, so two within one second cannot be told apart", full, instant)
	}
	if len(existing) > 1 && mode == modeReplaceOne {
		return fmt.Errorf(
			"%s already holds %d captures, which the server replays in order as a sequence; "+
				"pass -append to add to it or -replace to discard it", full, len(existing))
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, host+raw, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	name := instant
	if resp.StatusCode != http.StatusOK {
		name += fmt.Sprintf(".%d", resp.StatusCode)
	}
	name += ".json"

	// Only now that a response is in hand, so a failed request cannot cost the
	// capture that was already there.
	if mode != modeAppend {
		for _, old := range existing {
			if err := os.Remove(filepath.Join(full, old)); err != nil {
				return fmt.Errorf("replacing %s: %w", old, err)
			}
			slog.Info("replaced an earlier capture", "path", raw, "was", old)
		}
	}

	file := filepath.Join(full, name)
	if err := os.WriteFile(file, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}

	// Two shapes worth noticing now rather than as a puzzle later. An empty
	// array reads as success and is not. A 200 that is not JSON at all is an
	// upstream error page or a captcha, and committing one would fail at the
	// far end of a decode with nothing pointing back here.
	if resp.StatusCode == http.StatusOK {
		if trimmed := strings.TrimSpace(string(body)); trimmed == "[]" {
			slog.Warn("captured an empty array; a sync over this will do nothing and report success",
				"path", raw, "file", file)
		} else if !json.Valid(body) {
			slog.Warn("captured a 200 whose body is not JSON; this is probably an error page",
				"path", raw, "file", file)
		}
	}

	slog.Info("captured", "path", raw, "status", resp.StatusCode, "bytes", len(body), "file", file)
	return nil
}

// routeCaptures lists the capture files already on a route.
func routeCaptures(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}
