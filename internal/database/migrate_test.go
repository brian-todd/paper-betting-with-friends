package database

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/brian/paper-betting-with-friends/migrations"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// migrationName matches the golang-migrate convention the CLI and the embedded
// source both depend on.
var migrationName = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

// TestEmbeddedMigrationsAreWellFormed checks the set the server will apply to a
// production database at boot. Nothing here needs a database, and a mistake in
// any of it is only otherwise discovered during a deploy.
func TestEmbeddedMigrationsAreWellFormed(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}

	directions := make(map[int]map[string]bool)
	names := make(map[int]string)

	for _, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			t.Errorf("%s does not match NNNNNN_description.{up,down}.sql", entry.Name())
			continue
		}

		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Errorf("%s has an unparseable version: %v", entry.Name(), err)
			continue
		}

		if directions[version] == nil {
			directions[version] = make(map[string]bool)
			names[version] = match[2]
		}
		if names[version] != match[2] {
			t.Errorf("version %d has two descriptions: %q and %q", version, names[version], match[2])
		}
		if directions[version][match[3]] {
			t.Errorf("version %d has more than one %s migration", version, match[3])
		}
		directions[version][match[3]] = true

		// An empty file applies cleanly and does nothing, which is the worst
		// way for a rollback to fail.
		info, err := entry.Info()
		if err != nil {
			t.Errorf("stat %s: %v", entry.Name(), err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", entry.Name())
		}
	}

	if len(directions) == 0 {
		t.Fatal("no migrations were embedded")
	}

	// Every version must be reversible, or a bad deploy has no way back.
	for version, dirs := range directions {
		if !dirs["up"] {
			t.Errorf("version %d has no up migration", version)
		}
		if !dirs["down"] {
			t.Errorf("version %d has no down migration", version)
		}
	}

	// Versions must run 1..N with no gaps or duplicates: golang-migrate steps
	// through them in order, and a gap means a rollback stops early.
	versions := make([]int, 0, len(directions))
	for version := range directions {
		versions = append(versions, version)
	}
	sort.Ints(versions)

	for i, version := range versions {
		if version != i+1 {
			t.Errorf("migration versions are not sequential: got %d at position %d", version, i+1)
		}
	}
}

// TestEmbeddedMigrationsLoadAsASource runs the embedded files through the same
// driver Migrate uses, so a name the regex above tolerates but golang-migrate
// rejects still fails here.
func TestEmbeddedMigrationsLoadAsASource(t *testing.T) {
	driver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("iofs.New() error = %v", err)
	}
	defer driver.Close()

	version, err := driver.First()
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if version != 1 {
		t.Errorf("first version = %d, want 1", version)
	}

	// Walk the chain the way migrate does. iofs reports the end of the chain as
	// a not-exist error rather than a sentinel of its own.
	walked := 1
	for {
		next, err := driver.Next(version)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			t.Fatalf("Next(%d) error = %v", version, err)
		}
		version = next
		walked++
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}
	// One up and one down per version, which the test above already enforces.
	if want := len(entries) / 2; walked != want {
		t.Errorf("walked %d migrations, want %d", walked, want)
	}

	// Present is not the same as readable.
	if _, _, err := driver.ReadUp(1); err != nil {
		t.Errorf("ReadUp(1) error = %v", err)
	}
	if _, _, err := driver.ReadDown(1); err != nil {
		t.Errorf("ReadDown(1) error = %v", err)
	}
}
