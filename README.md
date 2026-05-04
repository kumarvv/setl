# SETL — Simple ETL Tool

A CLI-based ETL tool written in Go for transferring data between relational databases. Supports full and incremental loads, multi-threaded execution, and source partitioning for high-throughput pipelines.

## Features

- **Multiple databases** — Oracle, MySQL, Microsoft SQL Server, PostgreSQL
- **Full loads** — truncates the target table and reloads all rows
- **Incremental loads** — tracks a watermark column and only loads new/changed rows
- **Partitioned reads** — splits the source range into N slices and reads them in parallel
- **Worker pool** — bounded concurrency; runs multiple tables simultaneously
- **Batch commits** — commits every N rows to keep transactions manageable
- **Dual logging** — timestamped output to both stdout and a log file
- **Dry-run mode** — prints planned actions without writing any data
- **Multiple configs** — pass several YAML files in one invocation

---

## Requirements

- Go 1.22+
- Network access to source and target databases
- No Oracle client libraries required (uses the pure-Go `go-ora` driver)

---

## Installation

```bash
git clone https://github.com/kumarvv/setl.git
cd setl
go build -o setl .
```

Or install directly:

```bash
go install github.com/kumarvv/setl@latest
```

---

## Usage

```
setl [flags] config.yaml [config2.yaml ...]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-log <path>` | `setl.log` | Log file path |
| `-dry-run` | false | Print actions without writing data |
| `-workers <n>` | from config | Override `max_workers` from config |
| `-tables <names>` | all | Comma-separated table names to run |
| `-debug` | false | Enable verbose debug logging |
| `-version` | — | Print version and exit |

### Examples

```bash
# Run all tables in a config
setl config.yaml

# Preview without writing anything
setl -dry-run config.yaml

# Run only two specific tables
setl -tables ports,terminals config.yaml

# Override parallelism and log file
setl -workers 8 -log /var/log/etl.log config.yaml

# Run multiple config files in sequence
setl config1.yaml config2.yaml

# Verbose debug output
setl -debug config.yaml
```

---

## Configuration

Each YAML config file defines one source database, one target database, global settings, and a list of tables to transfer.

### Full reference

```yaml
source:
  type: oracle            # oracle | mysql | mssql | postgres
  host: localhost
  port: 1521
  database: FREEPDB1
  username: app_user
  password: secret

target:
  type: postgres
  host: localhost
  port: 5432
  database: warehouse
  username: etl_user
  password: secret

config:
  batch_commit_size: 1000   # rows per transaction commit (default: 1000)
  max_workers: 4            # max concurrent table workers (default: 4)
  truncate_method: truncate # truncate | delete  (full loads only, default: truncate)

tables:
  - <logical_name>:
      source: <sql_file.sql or table_name>
      target: <target_table_name>
      load_type: full | incremental
      key: <watermark_column>         # required for incremental
      partition_column: <column>      # optional: enables parallel partitioned reads
      partition_count: 4              # number of partitions (default: 4)
```

### `source` field

The `source` field accepts either:
- A **`.sql` file path** — the file contents are used as the source query
- A **bare table name** — expands to `SELECT * FROM <name>`

### `load_type` options

| Value | Behaviour |
|---|---|
| `full` | Truncates (or deletes) the target table, then inserts all rows from the source |
| `incremental` | Reads only rows where `key > last_watermark`; appends to the target |

### Watermarks

Watermarks for incremental loads are stored in `.setl_watermarks.json` in the working directory. On the first run (no watermark), all rows are loaded. After each successful run the max value of `key` is saved and used as the filter on the next run.

```json
{
  "terminals": "2024-11-15T08:30:00Z",
  "events":    42891
}
```

### Partitioning

Setting `partition_column` and `partition_count` splits the source into N equal numeric ranges and reads each range in a separate goroutine. This is most useful for large tables with a numeric surrogate key.

- The partition column must be **numeric** (integer or float).
- Partitioning works for both `full` and `incremental` load types.
- Full loads still truncate the target once before all partition goroutines start.

---

## Sample config files

### Oracle → PostgreSQL (mixed load types)

```yaml
source:
  type: oracle
  host: db-oracle.internal
  port: 1521
  database: FREEPDB1
  username: tacs_dev
  password: pwd

target:
  type: postgres
  host: db-postgres.internal
  port: 5432
  database: local_dev
  username: vkumar
  password: password

config:
  batch_commit_size: 500
  max_workers: 4

tables:
  - ports:
      source: sql/itp040.sql
      target: ports
      load_type: full

  - terminals:
      source: sql/itp130.sql
      target: terminals
      load_type: incremental
      key: updated_at
```

### MySQL → PostgreSQL (partitioned full load)

```yaml
source:
  type: mysql
  host: localhost
  port: 3306
  database: orders_db
  username: reader
  password: secret

target:
  type: postgres
  host: localhost
  port: 5432
  database: warehouse
  username: loader
  password: secret

config:
  batch_commit_size: 2000
  max_workers: 8

tables:
  - orders:
      source: SELECT * FROM orders WHERE status = 'COMPLETE'
      target: stg_orders
      load_type: full
      partition_column: order_id
      partition_count: 8

  - order_items:
      source: order_items
      target: stg_order_items
      load_type: incremental
      key: created_at
      partition_column: item_id
      partition_count: 4
```

### SQL Server → PostgreSQL

```yaml
source:
  type: mssql
  host: sqlserver.internal
  port: 1433
  database: OperationsDB
  username: etl_reader
  password: secret

target:
  type: postgres
  host: localhost
  port: 5432
  database: analytics
  username: loader
  password: secret

config:
  batch_commit_size: 1000
  max_workers: 4
  truncate_method: delete   # use DELETE instead of TRUNCATE for full loads

tables:
  - customers:
      source: sql/customers.sql
      target: dim_customers
      load_type: full

  - transactions:
      source: sql/transactions.sql
      target: fact_transactions
      load_type: incremental
      key: transaction_date
```

---

## SQL source files

When `source` ends in `.sql`, SETL reads the file and uses it as the source query. The file should contain a single `SELECT` statement without a trailing semicolon (or with one — SETL strips it automatically).

**sql/itp040.sql**
```sql
SELECT
    port_code,
    port_name,
    country_code,
    created_at,
    updated_at
FROM itp040_ports
WHERE active_flag = 'Y'
```

**sql/itp130.sql**
```sql
SELECT
    terminal_id,
    terminal_code,
    terminal_name,
    port_code,
    updated_at
FROM itp130_terminals
```

---

## Supported databases

| Database | Driver | Type string |
|---|---|---|
| Oracle | `github.com/sijms/go-ora/v2` (pure Go) | `oracle` |
| MySQL | `github.com/go-sql-driver/mysql` | `mysql` |
| Microsoft SQL Server | `github.com/microsoft/go-mssqldb` | `mssql` or `sqlserver` |
| PostgreSQL | `github.com/lib/pq` | `postgres` or `postgresql` |

---

## Project structure

```
setl/
├── main.go                    Entry point, CLI flag parsing
├── go.mod
├── Makefile
├── internal/
│   ├── config/config.go       YAML parsing and validation
│   ├── db/db.go               Database factory, placeholder styles
│   ├── etl/
│   │   ├── engine.go          Worker-pool orchestrator
│   │   └── loader.go          Full, incremental, and partitioned load logic
│   └── logger/logger.go       Dual-writer logger (stdout + file)
└── examples/
    └── config.yaml            Annotated sample config
```

---

## Building

```bash
# Build binary
make build

# Run with example config
make run

# Tidy dependencies
make tidy

# Clean build artifacts
make clean
```

---

## Limitations

- Partition column must be **numeric** (integer or float). Date/timestamp partitioning is not yet supported.
- Incremental loads append new rows only. If rows in the source can be updated (not just inserted), use a target table with a unique constraint and manage deduplication separately.
- SQL source files must contain a single `SELECT` statement with no trailing semicolon (SETL strips one if present).
- The tool reads all columns returned by the source query and inserts them into the target table. Column names must match between source query output and target table.
