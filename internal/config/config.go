package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type DBConfig struct {
	Type     string `yaml:"type"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Schema   string `yaml:"schema,omitempty"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Settings maps to the "config:" block in the YAML file.
type Settings struct {
	BatchCommitSize int    `yaml:"batch_commit_size"`
	MaxWorkers      int    `yaml:"max_workers"`
	TruncateMethod  string `yaml:"truncate_method"` // "truncate" (default) or "delete"
}

// TableConfig holds per-table ETL options.
type TableConfig struct {
	// Source is a .sql file path or a bare table name.
	Source string `yaml:"source"`
	// Target is the destination table name.
	Target string `yaml:"target"`
	// LoadType is "full" or "incremental".
	LoadType string `yaml:"load_type"`
	// Key is the watermark column used for incremental loads.
	Key string `yaml:"key,omitempty"`
	// PartitionColumn enables parallel partitioned reads on this numeric column.
	PartitionColumn string `yaml:"partition_column,omitempty"`
	// PartitionCount controls how many parallel partitions to run (default 4).
	PartitionCount int `yaml:"partition_count,omitempty"`
}

type Table struct {
	Name   string
	Config TableConfig
}

type Config struct {
	Source   DBConfig `yaml:"source"`
	Target   DBConfig `yaml:"target"`
	Settings Settings `yaml:"config"`
	// ConfigFile is the path the config was loaded from (used for resolving relative SQL paths).
	ConfigFile string
	tables     []Table
}

// rawConfig mirrors the YAML structure; tables are kept as raw nodes for flexible parsing.
type rawConfig struct {
	Source   DBConfig    `yaml:"source"`
	Target   DBConfig    `yaml:"target"`
	Settings Settings    `yaml:"config"`
	Tables   []yaml.Node `yaml:"tables"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg := &Config{
		Source:     raw.Source,
		Target:     raw.Target,
		Settings:   raw.Settings,
		ConfigFile: path,
	}

	// Apply defaults.
	if cfg.Settings.BatchCommitSize <= 0 {
		cfg.Settings.BatchCommitSize = 1000
	}
	if cfg.Settings.MaxWorkers <= 0 {
		cfg.Settings.MaxWorkers = 4
	}
	if cfg.Settings.TruncateMethod == "" {
		cfg.Settings.TruncateMethod = "truncate"
	}

	if err := validateDB("source", cfg.Source); err != nil {
		return nil, err
	}
	if err := validateDB("target", cfg.Target); err != nil {
		return nil, err
	}

	// Each YAML list item is a mapping with a single key (the logical table name).
	// Example:
	//   - ports:
	//       source: itp040.sql
	//       target: ports
	for _, node := range raw.Tables {
		if node.Kind != yaml.MappingNode || len(node.Content) < 2 {
			return nil, fmt.Errorf("%s: invalid table entry (expected mapping)", path)
		}

		name := node.Content[0].Value
		var tc TableConfig
		if err := node.Content[1].Decode(&tc); err != nil {
			return nil, fmt.Errorf("%s: table %q: %w", path, name, err)
		}

		if tc.LoadType == "" {
			tc.LoadType = "full"
		}
		tc.LoadType = strings.ToLower(tc.LoadType)

		if tc.PartitionColumn != "" && tc.PartitionCount <= 0 {
			tc.PartitionCount = 4
		}

		if err := validateTable(name, tc); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		cfg.tables = append(cfg.tables, Table{Name: name, Config: tc})
	}

	if len(cfg.tables) == 0 {
		return nil, fmt.Errorf("%s: no tables defined", path)
	}

	return cfg, nil
}

func (c *Config) Tables() []Table {
	return c.tables
}

func validateDB(label string, cfg DBConfig) error {
	if cfg.Type == "" {
		return fmt.Errorf("%s.type is required", label)
	}
	if cfg.Host == "" {
		return fmt.Errorf("%s.host is required", label)
	}
	if cfg.Port == 0 {
		return fmt.Errorf("%s.port is required", label)
	}
	return nil
}

func validateTable(name string, tc TableConfig) error {
	if tc.Source == "" {
		return fmt.Errorf("table %q: source is required", name)
	}
	if tc.Target == "" {
		return fmt.Errorf("table %q: target is required", name)
	}
	switch tc.LoadType {
	case "full", "incremental":
	default:
		return fmt.Errorf("table %q: load_type must be 'full' or 'incremental', got %q", name, tc.LoadType)
	}
	if tc.LoadType == "incremental" && tc.Key == "" {
		return fmt.Errorf("table %q: incremental load requires 'key' field", name)
	}
	return nil
}
