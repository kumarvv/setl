# SETL — Simple ETL Tool

[![Tests](https://github.com/kumarvv/setl/actions/workflows/test.yml/badge.svg)](https://github.com/kumarvv/setl/actions/workflows/test.yml)
[![Lint](https://github.com/kumarvv/setl/actions/workflows/lint.yml/badge.svg)](https://github.com/kumarvv/setl/actions/workflows/lint.yml)

A CLI-based ETL tool written in Go for transferring data between relational databases. Supports full and incremental loads, multi-threaded execution, and source partitioning for high-throughput pipelines.

## Features

- **Multiple databases** — Oracle, MySQL, Microsoft SQL Server, PostgreSQL
- **Central credential store** — database credentials live in `~/.setl/config.yml`, separate from ETL configs
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

## Setup

### 1. Create the global database registry

All database credentials are stored in a single file that is never committed to source control.

```bash
mkdir -p ~/.setl
cp examples/global_config.yml ~/.setl/config.yml
chmod 600 ~/.setl/config.yml   # restrict to your user only
```

Edit `~/.setl/config.yml` and add your databases:

```yaml
databases:
  oracle_dev:
    type: oracle
    host: localhost
    port: 1521
    database: FREE
    username: tacs_dev
    password: your_password

  postgres_local:
    type: postgres
    host: localhost
    port: 5432
    database: local_dev
    username: vkumar
    password: your_password

  mysql_app:
    type: mysql
    host: mysql.internal
    port: 3306
    database: app_db
    username: reader
    password: your_password

  mssql_ops:
    type: mssql
    host: sqlserver.internal
    port: 1433
    database: OperationsDB
    username: etl_reader
    password: your_password
```

Each key (e.g. `oracle_dev`) becomes the name you reference in ETL config files.

### 2. Write an ETL config

ETL configs reference databases by name — no credentials in the file:

```yaml
source: oracle_dev       # name from ~/.setl/config.yml
target: postgres_local   # name from ~/.setl/config.yml

config:
  batch_commit_size: 1000
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

### 3. Run

```bash
setl config.yaml
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
# Run all tables
setl config.yaml

# Preview without writing anything
setl -dry-run config.yaml

# Run only two specific tables
setl -tables ports,terminals config.yaml

# Override parallelism and log path
setl -workers 8 -log /var/log/etl.log config.yaml

# Run multiple config files in sequence
setl config1.yaml config2.yaml

# Verbose debug output
setl -debug config.yaml
```

---

## Global database registry (`~/.setl/config.yml`)

This file is loaded once at startup and shared across all ETL config files in the same invocation.

```yaml
databases:
  <name>:
    type:     oracle | mysql | mssql | postgres
    host:     <hostname or IP>
    port:     <port number>
    database: <database or service name>
    schema:   <optional schema>
    username: <username>
    password: <password>
```

| Field | Required | Description |
|---|---|---|
| `type` | yes | Database engine |
| `host` | yes | Hostname or IP address |
| `port` | yes | TCP port |
| `database` | yes | Database / service / SID name |
| `schema` | no | Default schema (optional) |
| `username` | yes | Login username |
| `password` | yes | Login password |

> **Security tip:** Keep `~/.setl/config.yml` at mode `600`. Never commit it to source control — add `.setl/` to your global `.gitignore`.

---

## ETL config file reference

### Top-level fields

```yaml
source: <database name>   # required — key from ~/.setl/config.yml
target: <database name>   # required — key from ~/.setl/config.yml

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

### `source` field (per-table)

Accepts either:
- A **`.sql` file path** — the file contents are used as the source query
- A **bare table name** — expands to `SELECT * FROM <name>`
- An **inline SQL expression** — e.g. `SELECT * FROM orders WHERE status = 'DONE'`

### `load_type` options

| Value | Behaviour |
|---|---|
| `full` | Truncates (or deletes) the target table, then inserts all rows from the source |
| `incremental` | Reads only rows where `key > last_watermark`; appends to the target |

### Watermarks

Watermarks for incremental loads are saved in `.setl_watermarks.json` in the working directory. On first run (no file) all rows are loaded. After each successful run the max value of `key` is saved as the next filter boundary.

```json
{
  "terminals": "2024-11-15T08:30:00Z",
  "events":    42891
}
```

### Partitioning

Setting `partition_column` splits the source into N equal numeric ranges read in parallel goroutines.

- The partition column must be **numeric** (integer or float).
- Works with both `full` and `incremental` load types.
- Full loads truncate the target once before all partition goroutines start.

---

## Sample configs

### Oracle → PostgreSQL (mixed load types)

```yaml
source: oracle_dev
target: postgres_local

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
source: mysql_app
target: postgres_warehouse

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
source: mssql_ops
target: postgres_warehouse

config:
  batch_commit_size: 1000
  max_workers: 4
  truncate_method: delete

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

When `source` ends in `.sql`, SETL reads the file and uses it as the source query. Write a single `SELECT` statement; a trailing semicolon is stripped automatically.

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
├── main.go                       Entry point, CLI flag parsing
├── go.mod
├── Makefile
├── internal/
│   ├── config/
│   │   ├── global.go             Loads ~/.setl/config.yml, resolves DB names
│   │   └── config.go             ETL config parsing and validation
│   ├── db/db.go                  Database factory, placeholder styles
│   ├── etl/
│   │   ├── engine.go             Worker-pool orchestrator
│   │   └── loader.go             Full, incremental, and partitioned load logic
│   └── logger/logger.go          Dual-writer logger (stdout + file)
└── examples/
    ├── global_config.yml         Template to copy to ~/.setl/config.yml
    └── config.yaml               Annotated ETL config example
```

---

## Building

```bash
make build   # produces ./setl
make run     # build + run examples/config.yaml
make tidy    # go mod tidy
make clean   # remove binary, log, and watermark files
```

---

## Limitations

- Partition column must be **numeric**. Date/timestamp partitioning is not yet supported.
- Incremental loads append new rows only. If rows can be updated as well as inserted, the target table needs a unique constraint and separate deduplication logic.
- SQL source files must contain a single `SELECT` statement (SETL strips one trailing semicolon if present).
- Column names returned by the source query must match the target table columns exactly.
