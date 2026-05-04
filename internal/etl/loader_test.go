package etl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kumarvv/setl/internal/config"
	"github.com/kumarvv/setl/internal/db"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// minLoader builds a loader with just enough config to exercise pure functions.
func minLoader(srcType, dstType string) *loader {
	return &loader{
		cfg: &config.Config{
			Source:   config.DBConfig{Type: srcType},
			Target:   config.DBConfig{Type: dstType},
			Settings: config.Settings{BatchCommitSize: 100},
		},
	}
}

// useWatermarkDir redirects the watermark file to a temp directory.
func useWatermarkDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := watermarkFile
	watermarkFile = filepath.Join(dir, "watermarks.json")
	t.Cleanup(func() { watermarkFile = orig })
}

// ── toFloat ───────────────────────────────────────────────────────────────────

func TestToFloat(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  float64
		isErr bool
	}{
		{"int64", int64(42), 42.0, false},
		{"int32", int32(7), 7.0, false},
		{"int", int(100), 100.0, false},
		{"float64", float64(3.14), 3.14, false},
		{"float32", float32(1.5), 1.5, false},
		{"uint64", uint64(999), 999.0, false},
		{"[]byte numeric", []byte("123.45"), 123.45, false},
		{"string numeric", "456.78", 456.78, false},
		{"[]byte non-numeric", []byte("abc"), 0, true},
		{"string non-numeric", "not_a_number", 0, true},
		{"unsupported type bool", true, 0, true},
		{"nil unsupported", nil, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toFloat(tt.input)
			if tt.isErr {
				if err == nil {
					t.Fatalf("expected error for input %v (%T), got %v", tt.input, tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Allow small float32 conversion imprecision.
			if got < tt.want-0.01 || got > tt.want+0.01 {
				t.Errorf("toFloat(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ── buildInsert ───────────────────────────────────────────────────────────────

func TestBuildInsert(t *testing.T) {
	tests := []struct {
		name  string
		table string
		cols  []string
		style db.PlaceholderStyle
		want  string
	}{
		{
			name:  "postgres single column",
			table: "orders",
			cols:  []string{"id"},
			style: db.StyleDollar,
			want:  "INSERT INTO orders (id) VALUES ($1)",
		},
		{
			name:  "postgres multiple columns",
			table: "ports",
			cols:  []string{"code", "name", "country"},
			style: db.StyleDollar,
			want:  "INSERT INTO ports (code, name, country) VALUES ($1, $2, $3)",
		},
		{
			name:  "mysql question marks",
			table: "t",
			cols:  []string{"a", "b"},
			style: db.StyleQuestion,
			want:  "INSERT INTO t (a, b) VALUES (?, ?)",
		},
		{
			name:  "mssql at-params",
			table: "dbo.t",
			cols:  []string{"x", "y"},
			style: db.StyleAt,
			want:  "INSERT INTO dbo.t (x, y) VALUES (@p1, @p2)",
		},
		{
			name:  "oracle colon-params",
			table: "schema.t",
			cols:  []string{"col1"},
			style: db.StyleColon,
			want:  "INSERT INTO schema.t (col1) VALUES (:1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildInsert(tt.table, tt.cols, tt.style)
			if got != tt.want {
				t.Errorf("buildInsert:\n  want %q\n  got  %q", tt.want, got)
			}
		})
	}
}

// ── sourceQuery ───────────────────────────────────────────────────────────────

func TestSourceQuery_BareTableName(t *testing.T) {
	l := minLoader("oracle", "postgres")
	tc := config.TableConfig{Source: "orders", Target: "stg_orders", LoadType: "full"}

	q, args, err := l.sourceQuery(tc, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != "SELECT * FROM orders" {
		t.Errorf("query: want %q, got %q", "SELECT * FROM orders", q)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestSourceQuery_SQLFile(t *testing.T) {
	dir := t.TempDir()
	sqlPath := filepath.Join(dir, "orders.sql")
	sqlContent := "SELECT id, name FROM orders WHERE active = 1;"
	if err := os.WriteFile(sqlPath, []byte(sqlContent), 0644); err != nil {
		t.Fatal(err)
	}

	l := minLoader("oracle", "postgres")
	tc := config.TableConfig{Source: sqlPath, Target: "stg_orders", LoadType: "full"}

	q, args, err := l.sourceQuery(tc, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Trailing semicolon should be stripped.
	if strings.HasSuffix(q, ";") {
		t.Errorf("trailing semicolon not stripped: %q", q)
	}
	if !strings.Contains(q, "SELECT id, name FROM orders") {
		t.Errorf("expected SQL content in query, got %q", q)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestSourceQuery_SQLFile_NotFound(t *testing.T) {
	l := minLoader("oracle", "postgres")
	tc := config.TableConfig{Source: "/no/such/file.sql", Target: "t", LoadType: "full"}

	_, _, err := l.sourceQuery(tc, "", nil)
	if err == nil {
		t.Fatal("expected error for missing SQL file")
	}
	if !strings.Contains(err.Error(), "reading SQL file") {
		t.Errorf("expected 'reading SQL file' in error, got: %v", err)
	}
}

func TestSourceQuery_Incremental_NoWatermark(t *testing.T) {
	l := minLoader("postgres", "postgres")
	tc := config.TableConfig{Source: "events", Target: "stg_events", LoadType: "incremental", Key: "event_ts"}

	// No watermark — should return base query unchanged.
	q, args, err := l.sourceQuery(tc, "event_ts", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != "SELECT * FROM events" {
		t.Errorf("want base query, got %q", q)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestSourceQuery_Incremental_WithWatermark(t *testing.T) {
	tests := []struct {
		name      string
		srcType   string
		watermark interface{}
		wantPH    string // expected placeholder
	}{
		{"postgres dollar", "postgres", "2024-01-15", "$1"},
		{"mysql question", "mysql", int64(100), "?"},
		{"oracle colon", "oracle", "2024-01-15", ":1"},
		{"mssql at", "mssql", int64(42), "@p1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := minLoader(tt.srcType, "postgres")
			tc := config.TableConfig{Source: "events", Target: "stg", LoadType: "incremental", Key: "ts"}

			q, args, err := l.sourceQuery(tc, "ts", tt.watermark)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(q, "WHERE ts >") {
				t.Errorf("expected WHERE clause in query, got %q", q)
			}
			if !strings.Contains(q, tt.wantPH) {
				t.Errorf("expected placeholder %q in query, got %q", tt.wantPH, q)
			}
			if len(args) != 1 || args[0] != tt.watermark {
				t.Errorf("args: want [%v], got %v", tt.watermark, args)
			}
		})
	}
}

// ── watermark read/write ──────────────────────────────────────────────────────

func TestWatermark_NotFound(t *testing.T) {
	useWatermarkDir(t)
	v, err := readWatermark("no_such_table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Errorf("expected nil watermark, got %v", v)
	}
}

func TestWatermark_WriteAndRead(t *testing.T) {
	useWatermarkDir(t)

	if err := writeWatermark("orders", "2024-06-01T12:00:00Z"); err != nil {
		t.Fatalf("writeWatermark: %v", err)
	}

	got, err := readWatermark("orders")
	if err != nil {
		t.Fatalf("readWatermark: %v", err)
	}
	if got != "2024-06-01T12:00:00Z" {
		t.Errorf("watermark: want %q, got %v", "2024-06-01T12:00:00Z", got)
	}
}

func TestWatermark_MultipleTablesCoexist(t *testing.T) {
	useWatermarkDir(t)

	if err := writeWatermark("t1", float64(100)); err != nil {
		t.Fatal(err)
	}
	if err := writeWatermark("t2", float64(200)); err != nil {
		t.Fatal(err)
	}

	v1, _ := readWatermark("t1")
	v2, _ := readWatermark("t2")
	if v1 == nil || v2 == nil {
		t.Fatal("expected both watermarks to be present")
	}
}

func TestWatermark_UpdateExisting(t *testing.T) {
	useWatermarkDir(t)

	_ = writeWatermark("orders", float64(50))
	_ = writeWatermark("orders", float64(150))

	got, err := readWatermark("orders")
	if err != nil {
		t.Fatalf("readWatermark: %v", err)
	}
	// JSON numbers come back as float64.
	if got.(float64) != 150 {
		t.Errorf("updated watermark: want 150, got %v", got)
	}
}

func TestWatermark_InvalidJSONFile(t *testing.T) {
	useWatermarkDir(t)
	if err := os.WriteFile(watermarkFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readWatermark("any")
	if err == nil {
		t.Fatal("expected error for invalid JSON watermark file")
	}
}

func TestWatermark_FileIsValidJSON(t *testing.T) {
	useWatermarkDir(t)

	_ = writeWatermark("tbl", "2024-12-31")

	data, err := os.ReadFile(watermarkFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("watermark file is not valid JSON: %v", err)
	}
}
