package etl

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kumarvv/setl/internal/config"
	"github.com/kumarvv/setl/internal/db"
	"github.com/kumarvv/setl/internal/logger"
)

var watermarkFile = ".setl_watermarks.json" // var so tests can redirect to a temp path

type loader struct {
	log    *logger.Logger
	src    *sql.DB
	dst    *sql.DB
	cfg    *config.Config
	dryRun bool
}

// ──────────────────────────────────────────────
// Full load
// ──────────────────────────────────────────────

func (l *loader) fullLoad(tbl config.Table) (int64, error) {
	query, args, err := l.sourceQuery(tbl.Config, "", nil)
	if err != nil {
		return 0, err
	}

	if !l.dryRun {
		if err := l.clearTarget(tbl); err != nil {
			return 0, err
		}
	}

	return l.transfer(tbl, query, args)
}

// ──────────────────────────────────────────────
// Incremental load
// ──────────────────────────────────────────────

func (l *loader) incrementalLoad(tbl config.Table) (int64, error) {
	wm, _ := readWatermark(tbl.Name)
	if wm != nil {
		l.log.Infof("[%s] Watermark: %v", tbl.Name, wm)
	} else {
		l.log.Infof("[%s] No watermark found — loading all records", tbl.Name)
	}

	query, args, err := l.sourceQuery(tbl.Config, tbl.Config.Key, wm)
	if err != nil {
		return 0, err
	}

	n, err := l.transfer(tbl, query, args)
	if err != nil {
		return n, err
	}

	if !l.dryRun && n > 0 {
		newWM, wmErr := l.maxKeyValue(tbl)
		if wmErr != nil {
			l.log.Warnf("[%s] Could not read new watermark: %v", tbl.Name, wmErr)
		} else if newWM != nil {
			if wmErr = writeWatermark(tbl.Name, newWM); wmErr != nil {
				l.log.Warnf("[%s] Could not save watermark: %v", tbl.Name, wmErr)
			} else {
				l.log.Infof("[%s] Watermark saved: %v", tbl.Name, newWM)
			}
		}
	}

	return n, nil
}

// ──────────────────────────────────────────────
// Partitioned load
// ──────────────────────────────────────────────

type pRange struct{ lo, hi float64 }

func (l *loader) runPartitioned(tbl config.Table) (int64, error) {
	// For full load, clear once before spawning partition goroutines.
	if tbl.Config.LoadType == "full" && !l.dryRun {
		if err := l.clearTarget(tbl); err != nil {
			return 0, err
		}
	}

	// For incremental, determine watermark so each partition filters accordingly.
	var wm interface{}
	if tbl.Config.LoadType == "incremental" {
		wm, _ = readWatermark(tbl.Name)
		if wm != nil {
			l.log.Infof("[%s] Watermark: %v", tbl.Name, wm)
		}
	}

	baseQuery, baseArgs, err := l.sourceQuery(tbl.Config, tbl.Config.Key, wm)
	if err != nil {
		return 0, err
	}

	ranges, err := l.partitionRanges(tbl, baseQuery, baseArgs)
	if err != nil {
		return 0, fmt.Errorf("partition ranges: %w", err)
	}
	if len(ranges) == 0 {
		l.log.Infof("[%s] Source is empty — nothing to partition", tbl.Name)
		return 0, nil
	}

	l.log.Infof("[%s] Launching %d partition(s) on column %q",
		tbl.Name, len(ranges), tbl.Config.PartitionColumn)

	var total int64
	errCh := make(chan error, len(ranges))
	var wg sync.WaitGroup

	for i, r := range ranges {
		wg.Add(1)
		go func(partNum int, pr pRange) {
			defer wg.Done()

			op := "<"
			if partNum == len(ranges)-1 {
				op = "<="
			}
			where := fmt.Sprintf("%s >= %g AND %s %s %g",
				tbl.Config.PartitionColumn, pr.lo,
				tbl.Config.PartitionColumn, op, pr.hi)

			l.log.Infof("[%s] Partition %d/%d: WHERE %s", tbl.Name, partNum+1, len(ranges), where)

			//noinspection SqlNoDataSourceInspection
			pQuery := fmt.Sprintf("SELECT * FROM (%s) __p%d WHERE %s", baseQuery, partNum, where)
			cnt, err := l.transfer(tbl, pQuery, baseArgs)
			if err != nil {
				errCh <- fmt.Errorf("partition %d: %w", partNum+1, err)
				return
			}
			atomic.AddInt64(&total, cnt)
			l.log.Infof("[%s] Partition %d/%d done: %d rows", tbl.Name, partNum+1, len(ranges), cnt)
		}(i, r)
	}

	wg.Wait()
	close(errCh)

	var msgs []string
	for e := range errCh {
		msgs = append(msgs, e.Error())
	}
	if len(msgs) > 0 {
		return total, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	// Update incremental watermark after all partitions succeed.
	if tbl.Config.LoadType == "incremental" && !l.dryRun && total > 0 {
		if newWM, err := l.maxKeyValue(tbl); err == nil && newWM != nil {
			if err := writeWatermark(tbl.Name, newWM); err == nil {
				l.log.Infof("[%s] Watermark saved: %v", tbl.Name, newWM)
			}
		}
	}

	return total, nil
}

func (l *loader) partitionRanges(tbl config.Table, baseQuery string, baseArgs []interface{}) ([]pRange, error) {
	col := tbl.Config.PartitionColumn
	n := tbl.Config.PartitionCount

	//noinspection SqlNoDataSourceInspection
	rangeSQL := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM (%s) __rng", col, col, baseQuery)
	l.log.Debugf("[%s] Range query: %s", tbl.Name, rangeSQL)

	row := l.src.QueryRow(rangeSQL, baseArgs...)
	var minV, maxV interface{}
	if err := row.Scan(&minV, &maxV); err != nil {
		return nil, fmt.Errorf("reading min/max for column %q: %w", col, err)
	}
	if minV == nil || maxV == nil {
		return nil, nil
	}

	minF, err := toFloat(minV)
	if err != nil {
		return nil, fmt.Errorf("partition column %q: %w", col, err)
	}
	maxF, err := toFloat(maxV)
	if err != nil {
		return nil, fmt.Errorf("partition column %q: %w", col, err)
	}

	l.log.Debugf("[%s] Partition column %q range: [%g, %g]", tbl.Name, col, minF, maxF)

	step := (maxF - minF) / float64(n)
	if step == 0 {
		return []pRange{{lo: minF, hi: maxF}}, nil
	}

	ranges := make([]pRange, n)
	for i := 0; i < n; i++ {
		ranges[i] = pRange{
			lo: minF + float64(i)*step,
			hi: minF + float64(i+1)*step,
		}
	}
	return ranges, nil
}

func toFloat(v interface{}) (float64, error) {
	switch n := v.(type) {
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case []byte:
		var f float64
		_, err := fmt.Sscanf(string(n), "%f", &f)
		return f, err
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err
	default:
		return 0, fmt.Errorf("cannot convert %T to float64 — partition column must be numeric", v)
	}
}

// ──────────────────────────────────────────────
// Core row-level transfer
// ──────────────────────────────────────────────

func (l *loader) transfer(tbl config.Table, query string, args []interface{}) (int64, error) {
	l.log.Debugf("[%s] Source query: %s", tbl.Name, query)

	rows, err := l.src.Query(query, args...)
	if err != nil {
		return 0, fmt.Errorf("source query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			l.log.Warnf("[%s] closing source rows: %v", tbl.Name, err)
		}
	}()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("reading column list: %w", err)
	}
	l.log.Debugf("[%s] Columns (%d): %s", tbl.Name, len(cols), strings.Join(cols, ", "))

	if l.dryRun {
		return l.countRows(tbl, rows, cols)
	}

	dstStyle := db.PlaceholderFor(l.cfg.Target.Type)
	insertSQL := buildInsert(tbl.Config.Target, cols, dstStyle)
	l.log.Debugf("[%s] Insert SQL: %s", tbl.Name, insertSQL)

	return l.batchInsert(tbl.Name, insertSQL, rows, cols)
}

func (l *loader) countRows(tbl config.Table, rows *sql.Rows, cols []string) (int64, error) {
	l.log.Infof("[%s] [DRY RUN] Target=%q  Columns: %s",
		tbl.Name, tbl.Config.Target, strings.Join(cols, ", "))

	buf := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range buf {
		ptrs[i] = &buf[i]
	}

	var n int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func (l *loader) batchInsert(name, insertSQL string, rows *sql.Rows, cols []string) (int64, error) {
	batchSize := l.cfg.Settings.BatchCommitSize

	scanBuf := make([]interface{}, len(cols))
	scanPtrs := make([]interface{}, len(cols))
	for i := range scanBuf {
		scanPtrs[i] = &scanBuf[i]
	}

	newTx := func() (*sql.Tx, *sql.Stmt, error) {
		tx, err := l.dst.Begin()
		if err != nil {
			return nil, nil, fmt.Errorf("begin transaction: %w", err)
		}
		stmt, err := tx.Prepare(insertSQL)
		if err != nil {
			_ = tx.Rollback()
			return nil, nil, fmt.Errorf("prepare insert: %w", err)
		}
		return tx, stmt, nil
	}

	tx, stmt, err := newTx()
	if err != nil {
		return 0, err
	}

	var total, inBatch int64

	for rows.Next() {
		if err := rows.Scan(scanPtrs...); err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("scan row: %w", err)
		}

		if _, err := stmt.Exec(scanBuf...); err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("insert row: %w", err)
		}

		total++
		inBatch++

		if int(inBatch) >= batchSize {
			_ = stmt.Close()
			if err := tx.Commit(); err != nil {
				return total, fmt.Errorf("commit batch: %w", err)
			}
			l.log.Infof("[%s] Batch committed: %d rows  (running total: %d)", name, inBatch, total)

			tx, stmt, err = newTx()
			if err != nil {
				return total, err
			}
			inBatch = 0
		}
	}

	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		return total, fmt.Errorf("reading source rows: %w", err)
	}

	_ = stmt.Close()
	if inBatch > 0 {
		if err := tx.Commit(); err != nil {
			return total, fmt.Errorf("final commit: %w", err)
		}
		l.log.Infof("[%s] Final batch committed: %d rows  (total: %d)", name, inBatch, total)
	} else {
		_ = tx.Rollback()
	}

	return total, nil
}

func buildInsert(table string, cols []string, style db.PlaceholderStyle) string {
	ph := make([]string, len(cols))
	for i := range cols {
		ph[i] = db.Placeholder(style, i+1)
	}
	//noinspection SqlNoDataSourceInspection
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(cols, ", "), strings.Join(ph, ", "))
}

// ──────────────────────────────────────────────
// Source query builder
// ──────────────────────────────────────────────

// sourceQuery builds the SQL to read from the source.
// If keyCol+watermark are provided the query is wrapped to filter incremental rows.
func (l *loader) sourceQuery(tc config.TableConfig, keyCol string, watermark interface{}) (string, []interface{}, error) {
	var base string

	if strings.HasSuffix(strings.ToLower(tc.Source), ".sql") {
		data, err := os.ReadFile(tc.Source)
		if err != nil {
			return "", nil, fmt.Errorf("reading SQL file %q: %w", tc.Source, err)
		}
		base = strings.TrimRight(strings.TrimSpace(string(data)), ";")
	} else {
		//noinspection SqlNoDataSourceInspection
		base = fmt.Sprintf("SELECT * FROM %s", tc.Source)
	}

	if keyCol == "" || watermark == nil {
		return base, nil, nil
	}

	srcStyle := db.PlaceholderFor(l.cfg.Source.Type)
	ph := db.Placeholder(srcStyle, 1)
	//noinspection SqlNoDataSourceInspection
	query := fmt.Sprintf("SELECT * FROM (%s) __etl_src WHERE %s > %s", base, keyCol, ph)
	return query, []interface{}{watermark}, nil
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func (l *loader) clearTarget(tbl config.Table) error {
	var stmt string
	switch l.cfg.Settings.TruncateMethod {
	case "delete":
		//noinspection SqlNoDataSourceInspection
		stmt = fmt.Sprintf("DELETE FROM %s", tbl.Config.Target)
	default:
		//noinspection SqlNoDataSourceInspection
		stmt = fmt.Sprintf("TRUNCATE TABLE %s", tbl.Config.Target)
	}
	l.log.Infof("[%s] Clearing target: %s", tbl.Name, stmt)
	_, err := l.dst.Exec(stmt)
	return err
}

func (l *loader) maxKeyValue(tbl config.Table) (interface{}, error) {
	tc := tbl.Config
	var base string
	if strings.HasSuffix(strings.ToLower(tc.Source), ".sql") {
		data, err := os.ReadFile(tc.Source)
		if err != nil {
			return nil, err
		}
		base = strings.TrimRight(strings.TrimSpace(string(data)), ";")
	} else {
		//noinspection SqlNoDataSourceInspection
		base = fmt.Sprintf("SELECT * FROM %s", tc.Source)
	}
	//noinspection SqlNoDataSourceInspection
	q := fmt.Sprintf("SELECT MAX(%s) FROM (%s) __etl_wm", tc.Key, base)
	var v interface{}
	return v, l.src.QueryRow(q).Scan(&v)
}

// ──────────────────────────────────────────────
// Watermark persistence  (JSON file, mutex-safe)
// ──────────────────────────────────────────────

var wmMu sync.Mutex

type wmStore map[string]interface{}

func readWatermark(name string) (interface{}, error) {
	wmMu.Lock()
	defer wmMu.Unlock()

	data, err := os.ReadFile(watermarkFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var store wmStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store[name], nil
}

func writeWatermark(name string, value interface{}) error {
	wmMu.Lock()
	defer wmMu.Unlock()

	var store wmStore
	if data, err := os.ReadFile(watermarkFile); err == nil {
		_ = json.Unmarshal(data, &store)
	}
	if store == nil {
		store = make(wmStore)
	}

	if t, ok := value.(time.Time); ok {
		store[name] = t.Format(time.RFC3339Nano)
	} else {
		store[name] = value
	}

	out, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(watermarkFile, out, 0644)
}
