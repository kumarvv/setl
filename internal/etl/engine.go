package etl

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/kumarvv/setl/internal/config"
	"github.com/kumarvv/setl/internal/db"
	"github.com/kumarvv/setl/internal/logger"
)

// Engine orchestrates table-level ETL jobs with a bounded worker pool.
type Engine struct {
	log         *logger.Logger
	dryRun      bool
	tableFilter map[string]bool
}

func NewEngine(log *logger.Logger, dryRun bool, tableFilter map[string]bool) *Engine {
	return &Engine{log: log, dryRun: dryRun, tableFilter: tableFilter}
}

type tableResult struct {
	name    string
	rows    int64
	elapsed time.Duration
	err     error
}

// Run executes all tables defined in cfg, respecting the worker-pool limit.
func (e *Engine) Run(cfg *config.Config) error {
	e.log.Infof("Source: %s://%s:%d/%s", cfg.Source.Type, cfg.Source.Host, cfg.Source.Port, cfg.Source.Database)
	e.log.Infof("Target: %s://%s:%d/%s", cfg.Target.Type, cfg.Target.Host, cfg.Target.Port, cfg.Target.Database)

	srcDB, err := db.Open(cfg.Source)
	if err != nil {
		return fmt.Errorf("source connection: %w", err)
	}
	defer srcDB.Close()
	e.log.Info("Source connected")

	dstDB, err := db.Open(cfg.Target)
	if err != nil {
		return fmt.Errorf("target connection: %w", err)
	}
	defer dstDB.Close()
	e.log.Info("Target connected")

	tables := e.applyFilter(cfg.Tables())
	if len(tables) == 0 {
		e.log.Warn("No tables matched — nothing to do")
		return nil
	}

	e.log.Infof("Processing %d table(s) | workers=%d | batch=%d",
		len(tables), cfg.Settings.MaxWorkers, cfg.Settings.BatchCommitSize)
	e.log.Info("─────────────────────────────────────────────────────────────────")

	sem := make(chan struct{}, cfg.Settings.MaxWorkers)
	results := make(chan tableResult, len(tables))
	var wg sync.WaitGroup

	for _, t := range tables {
		wg.Add(1)
		go func(tbl config.Table) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			e.log.Infof("[%s] ▶  load_type=%s", tbl.Name, tbl.Config.LoadType)
			rows, err := e.processTable(srcDB, dstDB, cfg, tbl)
			results <- tableResult{
				name:    tbl.Name,
				rows:    rows,
				elapsed: time.Since(start),
				err:     err,
			}
		}(t)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var failed int
	for r := range results {
		if r.err != nil {
			e.log.Errorf("[%-30s] ✗ FAILED   elapsed=%-10s error=%v",
				r.name, r.elapsed.Round(time.Millisecond), r.err)
			failed++
		} else {
			e.log.Infof("[%-30s] ✓ OK       elapsed=%-10s rows=%d",
				r.name, r.elapsed.Round(time.Millisecond), r.rows)
		}
	}
	e.log.Info("─────────────────────────────────────────────────────────────────")

	if failed > 0 {
		return fmt.Errorf("%d table(s) failed", failed)
	}
	return nil
}

func (e *Engine) applyFilter(tables []config.Table) []config.Table {
	if e.tableFilter == nil {
		return tables
	}
	var out []config.Table
	for _, t := range tables {
		if e.tableFilter[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

func (e *Engine) processTable(srcDB, dstDB *sql.DB, cfg *config.Config, tbl config.Table) (int64, error) {
	l := &loader{
		log:    e.log,
		src:    srcDB,
		dst:    dstDB,
		cfg:    cfg,
		dryRun: e.dryRun,
	}

	tc := tbl.Config

	// Partitioned mode takes precedence over load_type for reads;
	// full-load semantics (TRUNCATE) still apply when load_type=="full".
	if tc.PartitionColumn != "" && tc.PartitionCount > 1 {
		return l.runPartitioned(tbl)
	}

	switch tc.LoadType {
	case "full":
		return l.fullLoad(tbl)
	case "incremental":
		return l.incrementalLoad(tbl)
	default:
		return 0, fmt.Errorf("unknown load_type %q", tc.LoadType)
	}
}
