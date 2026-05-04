package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const globalConfigRelPath = ".setl/config.yml"

type globalRegistry struct {
	Databases map[string]DBConfig `yaml:"databases"`
}

// gcPathOverride is set during tests to redirect GlobalConfigPath to a temp file.
var gcPathOverride string

// GlobalConfigPath returns the absolute path to the global database registry.
func GlobalConfigPath() string {
	if gcPathOverride != "" {
		return gcPathOverride
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, globalConfigRelPath)
}

// singleton — loaded once per process run.
var (
	gcOnce sync.Once
	gc     *globalRegistry
	gcErr  error
)

func loadGlobalRegistry() (*globalRegistry, error) {
	gcOnce.Do(func() {
		gc, gcErr = loadRegistryFromPath(GlobalConfigPath())
	})
	return gc, gcErr
}

// loadRegistryFromPath reads and parses a registry file at the given path.
// Extracted so tests can call it directly without touching the singleton.
func loadRegistryFromPath(path string) (*globalRegistry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"%s not found\n\n"+
				"Create it with your database definitions, for example:\n\n"+
				"  databases:\n"+
				"    my_oracle:\n"+
				"      type: oracle\n"+
				"      host: localhost\n"+
				"      port: 1521\n"+
				"      database: FREE\n"+
				"      username: user\n"+
				"      password: pass\n"+
				"    my_postgres:\n"+
				"      type: postgres\n"+
				"      host: localhost\n"+
				"      port: 5432\n"+
				"      database: mydb\n"+
				"      username: user\n"+
				"      password: pass\n\n"+
				"Secure the file:  chmod 600 %s",
			path, path,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("reading global config %s: %w", path, err)
	}

	var reg globalRegistry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing global config %s: %w", path, err)
	}
	if reg.Databases == nil {
		reg.Databases = make(map[string]DBConfig)
	}
	return &reg, nil
}

// resolveDB looks up a named database from the global singleton registry.
func resolveDB(name string) (DBConfig, error) {
	reg, err := loadGlobalRegistry()
	if err != nil {
		return DBConfig{}, err
	}
	return resolveDBFromRegistry(reg, name)
}

// resolveDBFromRegistry looks up a named database from an arbitrary registry.
// Used directly by tests to bypass the singleton.
func resolveDBFromRegistry(reg *globalRegistry, name string) (DBConfig, error) {
	db, ok := reg.Databases[name]
	if !ok {
		return DBConfig{}, fmt.Errorf(
			"database %q not found in %s\n\nAvailable: %s",
			name, GlobalConfigPath(), availableDBs(reg),
		)
	}
	return db, nil
}

func availableDBs(reg *globalRegistry) string {
	if len(reg.Databases) == 0 {
		return "(none defined)"
	}
	names := make([]string, 0, len(reg.Databases))
	for k := range reg.Databases {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
