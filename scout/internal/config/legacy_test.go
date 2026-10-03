package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func useTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if home, err := os.UserHomeDir(); err == nil && filepath.Dir(DefaultConfigDir()) == filepath.Join(home, "Library", "Application Support") {
		t.Skip("macOS config folder ignores XDG_CONFIG_HOME")
	}
}

func TestMigrateLegacyDatabaseRenamesFiles(t *testing.T) {
	useTempConfigDir(t)
	oldDB := filepath.Join(DefaultConfigDir(), legacyDatabaseName)
	writeTestFile(t, oldDB, "db")
	writeTestFile(t, oldDB+"-wal", "wal")

	migrated, err := MigrateLegacyDatabase()
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	if got := readTestFile(t, AppDatabasePath()); got != "db" {
		t.Fatalf("database = %q", got)
	}
	if got := readTestFile(t, AppDatabasePath()+"-wal"); got != "wal" {
		t.Fatalf("wal = %q", got)
	}
	if _, err := os.Stat(oldDB); !os.IsNotExist(err) {
		t.Fatalf("old database still present: %v", err)
	}
	if migrated, err := MigrateLegacyDatabase(); err != nil || migrated {
		t.Fatalf("second run: migrated=%v err=%v", migrated, err)
	}
}

func TestMigrateLegacyDatabaseKeepsCurrentDatabase(t *testing.T) {
	useTempConfigDir(t)
	writeTestFile(t, filepath.Join(DefaultConfigDir(), legacyDatabaseName), "old")
	writeTestFile(t, AppDatabasePath(), "current")
	if migrated, err := MigrateLegacyDatabase(); err != nil || migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	if got := readTestFile(t, AppDatabasePath()); got != "current" {
		t.Fatalf("current database overwritten with %q", got)
	}
}
