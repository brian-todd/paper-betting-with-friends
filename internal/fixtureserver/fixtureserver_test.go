package fixtureserver_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
)

// write puts a capture into a temp tree and returns a Set over it.
func write(t *testing.T, files map[string]string) fixtures.Set {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return fixtures.FromDir(root)
}

// start runs a fixture server for the test's lifetime and returns its base URL.
func start(t *testing.T, opts ...fixtureserver.Option) (string, *fixtureserver.Server) {
	t.Helper()
	base, fake, stop, err := fixtureserver.Listen(fixtures.CFBD, opts...)
	if err != nil {
		t.Fatalf("starting the fixture server: %v", err)
	}
	t.Cleanup(stop)
	return base, fake
}

func get(t *testing.T, base, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// An unrouted path is the one case where being helpful would be a disaster.
// Every endpoint here returns a JSON array, so "[]" is the obvious answer and
// it is wrong: a sync over an empty array writes nothing, logs success, and
// looks exactly like a sync with nothing to write -- the bug the fixtures
// exist to find, rebuilt inside the tool meant to find it.
func TestUnroutedPathIsALoudFailureAndNeverAnEmptyArray(t *testing.T) {
	srvURL, _ := start(t, fixtureserver.WithFixtures(write(t, nil)))

	status, body := get(t, srvURL, "/teams")

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	if strings.TrimSpace(body) == "[]" {
		t.Fatal("served an empty array, which a sync cannot tell from success")
	}
	var parsed []any
	if json.Unmarshal([]byte(body), &parsed) == nil {
		t.Fatalf("served decodable JSON %q; a client must not be able to take this for data", body)
	}
	for _, want := range []string{"/teams", "cfbd/teams/_", "capture.sh"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, body)
		}
	}
}

// The convergence tests turn entirely on this: the same request has to answer
// differently the second time, because two feeds seeing one game at two
// moments is the thing under test.
func TestNthRequestGetsNthCapture(t *testing.T) {
	set := write(t, map[string]string{
		"cfbd/scoreboard/classification=fbs/20260905T204909Z.json": `["first"]`,
		"cfbd/scoreboard/classification=fbs/20260905T205903Z.json": `["second"]`,
		"cfbd/scoreboard/classification=fbs/20260905T211324Z.json": `["third"]`,
	})
	srvURL, fake := start(t, fixtureserver.WithFixtures(set))

	// Past the end it repeats the last, which is what a feed does when nothing
	// has changed -- a poll during a commercial break is not an error.
	want := []string{`["first"]`, `["second"]`, `["third"]`, `["third"]`}
	for i, w := range want {
		status, body := get(t, srvURL, "/scoreboard?classification=fbs")
		if status != http.StatusOK {
			t.Fatalf("request %d: status = %d", i+1, status)
		}
		if body != w {
			t.Errorf("request %d = %s, want %s", i+1, body, w)
		}
	}

	if n := len(fake.Requests()); n != 4 {
		t.Errorf("recorded %d requests, want 4", n)
	}
}

// Two callers spelling one query in different orders are one caller as far as
// the feed is concerned, so they must share a position in the sequence rather
// than each getting their own run from the top.
func TestQueryOrderDoesNotForkTheSequence(t *testing.T) {
	set := write(t, map[string]string{
		"cfbd/games/week=2&year=2026/20260905T204909Z.json": `["first"]`,
		"cfbd/games/week=2&year=2026/20260905T205903Z.json": `["second"]`,
	})
	srvURL, _ := start(t, fixtureserver.WithFixtures(set))

	if _, body := get(t, srvURL, "/games?year=2026&week=2"); body != `["first"]` {
		t.Fatalf("first request = %s", body)
	}
	if _, body := get(t, srvURL, "/games?week=2&year=2026"); body != `["second"]` {
		t.Errorf("the same query spelled the other way restarted the sequence: got %s", body)
	}
}

func TestExhaustIsErrorRefusesPastTheEnd(t *testing.T) {
	set := write(t, map[string]string{
		"cfbd/venues/_/20260905T204909Z.json": `["only"]`,
	})
	srvURL, _ := start(t, fixtureserver.WithFixtures(set), fixtureserver.ExhaustIsError())

	if status, _ := get(t, srvURL, "/venues"); status != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", status)
	}
	status, body := get(t, srvURL, "/venues")
	if status != http.StatusInternalServerError {
		t.Errorf("second request: status = %d, want 500", status)
	}
	if !strings.Contains(body, "exhausted") {
		t.Errorf("the refusal does not say why:\n%s", body)
	}
}

// A 502 is the documented failure of CFBD's /games and the reason
// relax-games-cadence.md exists; a body that is not JSON is doRequest's
// decode path, which has never met a real malformed response.
func TestFailuresAreFixturesToo(t *testing.T) {
	set := write(t, map[string]string{
		"cfbd/games/year=2026/20260905T204909Z.502.json": `{"error":"upstream"}`,
		"cfbd/venues/_/20260905T204909Z.bad":             `<html>maintenance</html>`,
	})
	srvURL, _ := start(t, fixtureserver.WithFixtures(set))

	if status, _ := get(t, srvURL, "/games?year=2026"); status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", status)
	}

	resp, err := http.Get(srvURL + "/venues")
	if err != nil {
		t.Fatalf("GET /venues: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json -- the point is a JSON promise the body breaks", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if json.Valid(body) {
		t.Errorf("body %q is valid JSON, so the decode path is not exercised", body)
	}
}
