package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// globalCfgYAML is a minimal registry used by config load tests.
const globalCfgYAML = `
databases:
  src_db:
    type: oracle
    host: src-host
    port: 1521
    database: SRCDB
    username: src_user
    password: src_pass
  dst_db:
    type: postgres
    host: dst-host
    port: 5432
    database: dstdb
    username: dst_user
    password: dst_pass
`

// setupGlobal writes the test registry and resets the singleton.
func setupGlobal(t *testing.T) {
	t.Helper()
	writeGlobalConfig(t, globalCfgYAML)
}

// writeETLConfig writes an ETL YAML config to a temp file and returns its path.
func writeETLConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "etl.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeETLConfig: %v", err)
	}
	return path
}

// ── Load ─────────────────────────────────────────────────────────────────────

func TestLoad_ValidFullConfig(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db

config:
  batch_commit_size: 500
  max_workers: 2

tables:
  - orders:
      source: orders.sql
      target: stg_orders
      load_type: full
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Source.Type != "oracle" {
		t.Errorf("source type: want oracle, got %q", cfg.Source.Type)
	}
	if cfg.Target.Type != "postgres" {
		t.Errorf("target type: want postgres, got %q", cfg.Target.Type)
	}
	if cfg.Settings.BatchCommitSize != 500 {
		t.Errorf("batch_commit_size: want 500, got %d", cfg.Settings.BatchCommitSize)
	}
	if cfg.Settings.MaxWorkers != 2 {
		t.Errorf("max_workers: want 2, got %d", cfg.Settings.MaxWorkers)
	}
	if len(cfg.Tables()) != 1 {
		t.Fatalf("want 1 table, got %d", len(cfg.Tables()))
	}
	tbl := cfg.Tables()[0]
	if tbl.Name != "orders" {
		t.Errorf("table name: want orders, got %q", tbl.Name)
	}
	if tbl.Config.LoadType != "full" {
		t.Errorf("load_type: want full, got %q", tbl.Config.LoadType)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - t:
      source: t
      target: t
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.BatchCommitSize != 1000 {
		t.Errorf("default batch_commit_size: want 1000, got %d", cfg.Settings.BatchCommitSize)
	}
	if cfg.Settings.MaxWorkers != 4 {
		t.Errorf("default max_workers: want 4, got %d", cfg.Settings.MaxWorkers)
	}
	if cfg.Settings.TruncateMethod != "truncate" {
		t.Errorf("default truncate_method: want truncate, got %q", cfg.Settings.TruncateMethod)
	}
}

func TestLoad_IncrementalLoad(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - events:
      source: events.sql
      target: stg_events
      load_type: incremental
      key: updated_at
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tbl := cfg.Tables()[0]
	if tbl.Config.LoadType != "incremental" {
		t.Errorf("load_type: want incremental, got %q", tbl.Config.LoadType)
	}
	if tbl.Config.Key != "updated_at" {
		t.Errorf("key: want updated_at, got %q", tbl.Config.Key)
	}
}

func TestLoad_PartitionedTable(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - orders:
      source: orders
      target: stg_orders
      load_type: full
      partition_column: order_id
      partition_count: 8
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tbl := cfg.Tables()[0]
	if tbl.Config.PartitionColumn != "order_id" {
		t.Errorf("partition_column: want order_id, got %q", tbl.Config.PartitionColumn)
	}
	if tbl.Config.PartitionCount != 8 {
		t.Errorf("partition_count: want 8, got %d", tbl.Config.PartitionCount)
	}
}

func TestLoad_PartitionCountDefaultsTo4(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - t:
      source: t
      target: t
      partition_column: id
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tables()[0].Config.PartitionCount != 4 {
		t.Errorf("default partition_count: want 4, got %d", cfg.Tables()[0].Config.PartitionCount)
	}
}

func TestLoad_LoadTypeDefaultsFull(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - t:
      source: t
      target: t
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tables()[0].Config.LoadType != "full" {
		t.Errorf("default load_type: want full, got %q", cfg.Tables()[0].Config.LoadType)
	}
}

func TestLoad_LoadTypeNormalisedToLower(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - t:
      source: t
      target: t
      load_type: FULL
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tables()[0].Config.LoadType != "full" {
		t.Errorf("normalised load_type: want full, got %q", cfg.Tables()[0].Config.LoadType)
	}
}

func TestLoad_MultipleTables(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables:
  - ports:
      source: ports.sql
      target: ports
      load_type: full
  - terminals:
      source: terminals.sql
      target: terminals
      load_type: incremental
      key: updated_at
  - vessels:
      source: vessels
      target: vessels
      load_type: full
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tables()) != 3 {
		t.Errorf("want 3 tables, got %d", len(cfg.Tables()))
	}
}

// ── Load error cases ─────────────────────────────────────────────────────────

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	// "{unclosed" is genuinely invalid YAML (unclosed flow mapping).
	path := writeETLConfig(t, "{unclosed: [")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("expected 'parsing' in error, got: %v", err)
	}
}

func TestLoad_MissingSource(t *testing.T) {
	// No global config needed — should fail before resolving.
	resetGlobal(t)
	path := writeETLConfig(t, `
target: dst_db
tables:
  - t:
      source: t
      target: t
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("expected 'source' in error, got: %v", err)
	}
}

func TestLoad_MissingTarget(t *testing.T) {
	resetGlobal(t)
	path := writeETLConfig(t, `
source: src_db
tables:
  - t:
      source: t
      target: t
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("expected 'target' in error, got: %v", err)
	}
}

func TestLoad_UnknownSourceDB(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: no_such_db
target: dst_db
tables:
  - t:
      source: t
      target: t
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown source DB")
	}
	if !strings.Contains(err.Error(), "no_such_db") {
		t.Errorf("expected DB name in error, got: %v", err)
	}
}

func TestLoad_UnknownTargetDB(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: no_such_db
tables:
  - t:
      source: t
      target: t
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown target DB")
	}
	if !strings.Contains(err.Error(), "no_such_db") {
		t.Errorf("expected DB name in error, got: %v", err)
	}
}

func TestLoad_NoTables(t *testing.T) {
	setupGlobal(t)
	path := writeETLConfig(t, `
source: src_db
target: dst_db
tables: []
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when tables list is empty")
	}
	if !strings.Contains(err.Error(), "no tables") {
		t.Errorf("expected 'no tables' in error, got: %v", err)
	}
}

// ── validateTable ─────────────────────────────────────────────────────────────

func TestValidateTable(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		tc        TableConfig
		wantErr   string
	}{
		{
			name:      "valid full load",
			tableName: "t",
			tc:        TableConfig{Source: "src", Target: "dst", LoadType: "full"},
		},
		{
			name:      "valid incremental",
			tableName: "t",
			tc:        TableConfig{Source: "src", Target: "dst", LoadType: "incremental", Key: "updated_at"},
		},
		{
			name:      "missing source",
			tableName: "t",
			tc:        TableConfig{Target: "dst", LoadType: "full"},
			wantErr:   "source is required",
		},
		{
			name:      "missing target",
			tableName: "t",
			tc:        TableConfig{Source: "src", LoadType: "full"},
			wantErr:   "target is required",
		},
		{
			name:      "invalid load_type",
			tableName: "t",
			tc:        TableConfig{Source: "src", Target: "dst", LoadType: "upsert"},
			wantErr:   "load_type must be",
		},
		{
			name:      "incremental without key",
			tableName: "t",
			tc:        TableConfig{Source: "src", Target: "dst", LoadType: "incremental"},
			wantErr:   "requires 'key' field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTable(tt.tableName, tt.tc)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// reset singleton state shared with global_test.go
var _ = func() bool {
	gcOnce = sync.Once{}
	gc = nil
	gcErr = nil
	return true
}()
