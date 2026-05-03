package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kumarvv/setl/internal/config"
	"github.com/kumarvv/setl/internal/etl"
	"github.com/kumarvv/setl/internal/logger"
)

var version = "1.0.0"

func main() {
	var (
		logFile     string
		dryRun      bool
		workers     int
		tableFilter string
		debug       bool
		showVersion bool
	)

	flag.StringVar(&logFile, "log", "setl.log", "Log file path")
	flag.BoolVar(&dryRun, "dry-run", false, "Print planned actions without writing data")
	flag.IntVar(&workers, "workers", 0, "Override max_workers from config (0 = use config value)")
	flag.StringVar(&tableFilter, "tables", "", "Comma-separated table names to run (default: all)")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.Usage = printUsage
	flag.Parse()

	if showVersion {
		_, _ = fmt.Printf("setl v%s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	log, err := logger.New(logFile, debug)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "ERROR: failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := log.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "ERROR: closing log file: %v\n", err)
		}
	}()

	log.Infof("SETL v%s — Simple ETL Tool", version)
	if dryRun {
		log.Info("*** DRY RUN MODE — no data will be written ***")
	}

	var filterSet map[string]bool
	if tableFilter != "" {
		filterSet = make(map[string]bool)
		for _, t := range strings.Split(tableFilter, ",") {
			filterSet[strings.TrimSpace(t)] = true
		}
		log.Infof("Table filter: %s", tableFilter)
	}

	exitCode := 0
	for _, configFile := range args {
		log.Infof("Loading config: %s", configFile)

		cfg, err := config.Load(configFile)
		if err != nil {
			log.Errorf("Config error (%s): %v", configFile, err)
			exitCode = 1
			continue
		}

		if workers > 0 {
			cfg.Settings.MaxWorkers = workers
		}

		engine := etl.NewEngine(log, dryRun, filterSet)
		if err := engine.Run(cfg); err != nil {
			log.Errorf("ETL failed for %s: %v", configFile, err)
			exitCode = 1
		}
	}

	if exitCode == 0 {
		log.Info("All ETL jobs completed successfully")
	} else {
		log.Error("ETL completed with errors — check log for details")
	}
	os.Exit(exitCode)
}

func printUsage() {
	_, _ = fmt.Fprintf(os.Stderr, `SETL v%s — Simple ETL Tool

Usage:
  setl [flags] config.yaml [config2.yaml ...]

Flags:
`, version)
	flag.PrintDefaults()
	_, _ = fmt.Fprintln(os.Stderr, `
Examples:
  setl config.yaml
  setl -dry-run config.yaml
  setl -debug -tables ports,terminals config.yaml
  setl -workers 8 -log /var/log/etl.log config.yaml
  setl config1.yaml config2.yaml`)
}
