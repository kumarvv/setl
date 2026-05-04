package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetGlobal resets the singleton so each test gets a clean slate.
func resetGlobal(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		gcOnce = sync.Once{}
		gc = nil
		gcErr = nil
		gcPathOverride = ""
	})
}

// writeGlobalConfig writes YAML to a temp file and configures the singleton path.
func writeGlobalConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeGlobalConfig: %v", err)
	}
	gcPathOverride = path
	gcOnce = sync.Once{}
	gc = nil
	gcErr = nil
	return path
}

// ── loadRegistryFromPath ────────────────────────────────────────────────────

func TestLoadRegistryFromPath_FileNotFound(t *testing.T) {
	_, err := loadRegistryFromPath("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestLoadRegistryFromPath_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("{unclosed: ["), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadRegistryFromPath(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing global config") {
		t.Errorf("expected 'parsing global config' in error, got: %v", err)
	}
}

func TestLoadRegistryFromPath_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := `
databases:
  oracle_dev:
    type: oracle
    host: localhost
    port: 1521
    database: FREE
    username: user
    password: pass
  pg_local:
    type: postgres
    host: localhost
    port: 5432
    database: mydb
    username: user
    password: pass
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	reg, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Databases) != 2 {
		t.Errorf("expected 2 databases, got %d", len(reg.Databases))
	}
	if reg.Databases["oracle_dev"].Type != "oracle" {
		t.Errorf("expected type 'oracle', got %q", reg.Databases["oracle_dev"].Type)
	}
	if reg.Databases["pg_local"].Port != 5432 {
		t.Errorf("expected port 5432, got %d", reg.Databases["pg_local"].Port)
	}
}

func TestLoadRegistryFromPath_EmptyDatabases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("databases:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Databases == nil {
		t.Error("expected non-nil Databases map for empty config")
	}
}

// ── resolveDBFromRegistry ───────────────────────────────────────────────────

func TestResolveDBFromRegistry_Found(t *testing.T) {
	reg := &globalRegistry{
		Databases: map[string]DBConfig{
			"pg": {Type: "postgres", Host: "localhost", Port: 5432, Database: "db", Username: "u", Password: "p"},
		},
	}
	got, err := resolveDBFromRegistry(reg, "pg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type != "postgres" {
		t.Errorf("expected type 'postgres', got %q", got.Type)
	}
	if got.Port != 5432 {
		t.Errorf("expected port 5432, got %d", got.Port)
	}
}

func TestResolveDBFromRegistry_NotFound(t *testing.T) {
	reg := &globalRegistry{
		Databases: map[string]DBConfig{
			"pg": {Type: "postgres"},
		},
	}
	_, err := resolveDBFromRegistry(reg, "missing_db")
	if err == nil {
		t.Fatal("expected error for missing database, got nil")
	}
	if !strings.Contains(err.Error(), "missing_db") {
		t.Errorf("expected database name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "pg") {
		t.Errorf("expected available databases in error, got: %v", err)
	}
}

func TestResolveDBFromRegistry_EmptyRegistry(t *testing.T) {
	reg := &globalRegistry{Databases: map[string]DBConfig{}}
	_, err := resolveDBFromRegistry(reg, "anything")
	if err == nil {
		t.Fatal("expected error for empty registry, got nil")
	}
	if !strings.Contains(err.Error(), "none defined") {
		t.Errorf("expected 'none defined' in error, got: %v", err)
	}
}

// ── availableDBs ────────────────────────────────────────────────────────────

func TestAvailableDBs_Empty(t *testing.T) {
	reg := &globalRegistry{Databases: map[string]DBConfig{}}
	got := availableDBs(reg)
	if got != "(none defined)" {
		t.Errorf("expected '(none defined)', got %q", got)
	}
}

func TestAvailableDBs_Sorted(t *testing.T) {
	reg := &globalRegistry{
		Databases: map[string]DBConfig{
			"zebra": {},
			"alpha": {},
			"mango": {},
		},
	}
	got := availableDBs(reg)
	if got != "alpha, mango, zebra" {
		t.Errorf("expected alphabetically sorted list, got %q", got)
	}
}

// ── resolveDB (integration with singleton) ──────────────────────────────────

func TestResolveDB_ViaGlobalConfig(t *testing.T) {
	resetGlobal(t)
	writeGlobalConfig(t, `
databases:
  my_db:
    type: mysql
    host: db.internal
    port: 3306
    database: appdb
    username: reader
    password: secret
`)
	got, err := resolveDB("my_db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type != "mysql" || got.Host != "db.internal" {
		t.Errorf("unexpected DBConfig: %+v", got)
	}
}

func TestResolveDB_GlobalConfigMissing(t *testing.T) {
	resetGlobal(t)
	gcPathOverride = "/no/such/file.yml"
	_, err := resolveDB("anything")
	if err == nil {
		t.Fatal("expected error when global config is missing")
	}
}
