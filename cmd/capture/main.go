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
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
	flag.Parse()

	logging.Setup("development")

	if err := run(*provider, *out, *timeout, flag.Args()); err != nil {
		slog.Error("capture failed", "error", err)
		os.Exit(1)
	}
}

func run(provider, out string, timeout time.Duration, paths []string) error {
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
		if err := capture(client, provider, host, apiKey, out, instant, raw); err != nil {
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

func capture(client *http.Client, provider, host, apiKey, out, instant, raw string) error {
	requestPath, rawQuery, _ := strings.Cut(strings.TrimPrefix(raw, host), "?")

	dir, err := fixtures.Dir(provider, requestPath, rawQuery)
	if err != nil {
		return err
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

	full := filepath.Join(out, filepath.FromSlash(dir))
	if err := os.MkdirAll(full, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", full, err)
	}
	file := filepath.Join(full, name)
	if err := os.WriteFile(file, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}

	// An empty array is the shape that reads as success and is not, so it is
	// worth a warning at the moment of capture rather than a puzzle later.
	if trimmed := strings.TrimSpace(string(body)); trimmed == "[]" {
		slog.Warn("captured an empty array; a sync over this will do nothing and report success",
			"path", raw, "file", file)
	}

	slog.Info("captured", "path", raw, "status", resp.StatusCode, "bytes", len(body), "file", file)
	return nil
}
