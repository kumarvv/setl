package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

const globalConfigRelPath = ".setl/config.yml"

type globalRegistry struct {
	Databases map[string]DBConfig `yaml:"databases"`
}

// GlobalConfigPath returns the absolute path to the global database registry.
func GlobalConfigPath() string {
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
		path := GlobalConfigPath()
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			gcErr = fmt.Errorf(
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
			return
		}
		if err != nil {
			gcErr = fmt.Errorf("reading global config %s: %w", path, err)
			return
		}

		var reg globalRegistry
		if err := yaml.Unmarshal(data, &reg); err != nil {
			gcErr = fmt.Errorf("parsing global config %s: %w", path, err)
			return
		}
		if reg.Databases == nil {
			reg.Databases = make(map[string]DBConfig)
		}
		gc = &reg
	})
	return gc, gcErr
}

// resolveDB looks up a named database from the global registry.
func resolveDB(name string) (DBConfig, error) {
	reg, err := loadGlobalRegistry()
	if err != nil {
		return DBConfig{}, err
	}
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
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}
