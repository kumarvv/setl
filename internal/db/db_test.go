package db

import (
	"strings"
	"testing"

	"github.com/kumarvv/setl/internal/config"
)

// ── buildDSN ─────────────────────────────────────────────────────────────────

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.DBConfig
		wantDriver string
		wantDSN    string
		wantErr    bool
	}{
		{
			name:       "oracle",
			cfg:        config.DBConfig{Type: "oracle", Host: "orahost", Port: 1521, Database: "FREE", Username: "user", Password: "pass"},
			wantDriver: "oracle",
			wantDSN:    "oracle://user:pass@orahost:1521/FREE",
		},
		{
			name:       "mysql",
			cfg:        config.DBConfig{Type: "mysql", Host: "myhost", Port: 3306, Database: "mydb", Username: "u", Password: "p"},
			wantDriver: "mysql",
			wantDSN:    "u:p@tcp(myhost:3306)/mydb?parseTime=true",
		},
		{
			name:       "mssql",
			cfg:        config.DBConfig{Type: "mssql", Host: "sqlhost", Port: 1433, Database: "ops", Username: "sa", Password: "pwd"},
			wantDriver: "sqlserver",
			wantDSN:    "sqlserver://sa:pwd@sqlhost:1433?database=ops",
		},
		{
			name:       "sqlserver alias",
			cfg:        config.DBConfig{Type: "sqlserver", Host: "sqlhost", Port: 1433, Database: "ops", Username: "sa", Password: "pwd"},
			wantDriver: "sqlserver",
			wantDSN:    "sqlserver://sa:pwd@sqlhost:1433?database=ops",
		},
		{
			name:       "postgres",
			cfg:        config.DBConfig{Type: "postgres", Host: "pghost", Port: 5432, Database: "pgdb", Username: "pguser", Password: "pgpass"},
			wantDriver: "postgres",
			wantDSN:    "postgres://pguser:pgpass@pghost:5432/pgdb?sslmode=disable",
		},
		{
			name:       "postgresql alias",
			cfg:        config.DBConfig{Type: "postgresql", Host: "pghost", Port: 5432, Database: "pgdb", Username: "pguser", Password: "pgpass"},
			wantDriver: "postgres",
			wantDSN:    "postgres://pguser:pgpass@pghost:5432/pgdb?sslmode=disable",
		},
		{
			name:    "unsupported type",
			cfg:     config.DBConfig{Type: "sqlite"},
			wantErr: true,
		},
		{
			name:    "empty type",
			cfg:     config.DBConfig{},
			wantErr: true,
		},
		{
			name:       "type case insensitive",
			cfg:        config.DBConfig{Type: "ORACLE", Host: "h", Port: 1521, Database: "d", Username: "u", Password: "p"},
			wantDriver: "oracle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, dsn, err := buildDSN(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got driver=%q dsn=%q", driver, dsn)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if driver != tt.wantDriver {
				t.Errorf("driver: want %q, got %q", tt.wantDriver, driver)
			}
			if tt.wantDSN != "" && dsn != tt.wantDSN {
				t.Errorf("DSN: want %q, got %q", tt.wantDSN, dsn)
			}
		})
	}
}

func TestBuildDSN_ErrorContainsTypeName(t *testing.T) {
	_, _, err := buildDSN(config.DBConfig{Type: "cassandra"})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "cassandra") {
		t.Errorf("error should mention the unsupported type, got: %v", err)
	}
}

// ── PlaceholderFor ────────────────────────────────────────────────────────────

func TestPlaceholderFor(t *testing.T) {
	tests := []struct {
		dbType string
		want   PlaceholderStyle
	}{
		{"postgres", StyleDollar},
		{"postgresql", StyleDollar},
		{"POSTGRES", StyleDollar},
		{"mssql", StyleAt},
		{"sqlserver", StyleAt},
		{"oracle", StyleColon},
		{"ORACLE", StyleColon},
		{"mysql", StyleQuestion},
		{"", StyleQuestion},
		{"unknown", StyleQuestion},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			got := PlaceholderFor(tt.dbType)
			if got != tt.want {
				t.Errorf("PlaceholderFor(%q) = %v, want %v", tt.dbType, got, tt.want)
			}
		})
	}
}

// ── Placeholder ───────────────────────────────────────────────────────────────

func TestPlaceholder(t *testing.T) {
	tests := []struct {
		style PlaceholderStyle
		n     int
		want  string
	}{
		{StyleQuestion, 1, "?"},
		{StyleQuestion, 5, "?"},
		{StyleDollar, 1, "$1"},
		{StyleDollar, 7, "$7"},
		{StyleAt, 1, "@p1"},
		{StyleAt, 3, "@p3"},
		{StyleColon, 1, ":1"},
		{StyleColon, 10, ":10"},
	}

	for _, tt := range tests {
		got := Placeholder(tt.style, tt.n)
		if got != tt.want {
			t.Errorf("Placeholder(%v, %d) = %q, want %q", tt.style, tt.n, got, tt.want)
		}
	}
}
