package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/kumarvv/setl/internal/config"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
)

// PlaceholderStyle selects the parameter binding syntax for a given database.
type PlaceholderStyle int

const (
	StyleQuestion PlaceholderStyle = iota // MySQL / SQLite: ?
	StyleDollar                           // PostgreSQL:     $1, $2 …
	StyleAt                               // SQL Server:     @p1, @p2 …
	StyleColon                            // Oracle:         :1, :2 …
)

// Open creates and verifies a database connection from the supplied config.
func Open(cfg config.DBConfig) (*sql.DB, error) {
	driver, dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Type, err)
	}

	if err := db.Ping(); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("connect to %s @ %s:%d: %w (also failed to close: %v)", cfg.Type, cfg.Host, cfg.Port, err, closeErr)
		}
		return nil, fmt.Errorf("connect to %s @ %s:%d: %w", cfg.Type, cfg.Host, cfg.Port, err)
	}

	return db, nil
}

func buildDSN(cfg config.DBConfig) (driver, dsn string, err error) {
	switch strings.ToLower(cfg.Type) {
	case "oracle":
		driver = "oracle"
		dsn = fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	case "mysql":
		driver = "mysql"
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	case "mssql", "sqlserver":
		driver = "sqlserver"
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	case "postgres", "postgresql":
		driver = "postgres"
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	default:
		err = fmt.Errorf("unsupported db type %q (oracle | mysql | mssql | postgres)", cfg.Type)
	}
	return
}

// PlaceholderFor returns the placeholder style required by the given DB type.
func PlaceholderFor(dbType string) PlaceholderStyle {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return StyleDollar
	case "mssql", "sqlserver":
		return StyleAt
	case "oracle":
		return StyleColon
	default:
		return StyleQuestion
	}
}

// Placeholder formats the n-th (1-based) bind parameter for the given style.
func Placeholder(style PlaceholderStyle, n int) string {
	switch style {
	case StyleDollar:
		return fmt.Sprintf("$%d", n)
	case StyleAt:
		return fmt.Sprintf("@p%d", n)
	case StyleColon:
		return fmt.Sprintf(":%d", n)
	default:
		return "?"
	}
}
