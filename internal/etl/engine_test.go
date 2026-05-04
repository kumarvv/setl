package etl

import (
	"testing"

	"github.com/kumarvv/setl/internal/config"
)

// ── applyFilter ───────────────────────────────────────────────────────────────

func makeTable(name string) config.Table {
	return config.Table{Name: name, Config: config.TableConfig{Source: name, Target: name, LoadType: "full"}}
}

func makeEngine(filter map[string]bool) *Engine {
	return &Engine{tableFilter: filter}
}

func tableNames(tables []config.Table) []string {
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = t.Name
	}
	return names
}

func TestApplyFilter_NilFilter_ReturnsAll(t *testing.T) {
	e := makeEngine(nil)
	tables := []config.Table{makeTable("a"), makeTable("b"), makeTable("c")}

	got := e.applyFilter(tables)
	if len(got) != 3 {
		t.Errorf("nil filter: want 3 tables, got %d: %v", len(got), tableNames(got))
	}
}

func TestApplyFilter_EmptyFilter_ReturnsNone(t *testing.T) {
	e := makeEngine(map[string]bool{})
	tables := []config.Table{makeTable("a"), makeTable("b")}

	got := e.applyFilter(tables)
	if len(got) != 0 {
		t.Errorf("empty filter: want 0 tables, got %d: %v", len(got), tableNames(got))
	}
}

func TestApplyFilter_MatchAll(t *testing.T) {
	filter := map[string]bool{"a": true, "b": true, "c": true}
	e := makeEngine(filter)
	tables := []config.Table{makeTable("a"), makeTable("b"), makeTable("c")}

	got := e.applyFilter(tables)
	if len(got) != 3 {
		t.Errorf("full match: want 3 tables, got %d: %v", len(got), tableNames(got))
	}
}

func TestApplyFilter_PartialMatch(t *testing.T) {
	filter := map[string]bool{"b": true}
	e := makeEngine(filter)
	tables := []config.Table{makeTable("a"), makeTable("b"), makeTable("c")}

	got := e.applyFilter(tables)
	if len(got) != 1 {
		t.Fatalf("partial match: want 1 table, got %d: %v", len(got), tableNames(got))
	}
	if got[0].Name != "b" {
		t.Errorf("partial match: want table 'b', got %q", got[0].Name)
	}
}

func TestApplyFilter_NoMatch(t *testing.T) {
	filter := map[string]bool{"x": true, "y": true}
	e := makeEngine(filter)
	tables := []config.Table{makeTable("a"), makeTable("b")}

	got := e.applyFilter(tables)
	if len(got) != 0 {
		t.Errorf("no match: want 0 tables, got %d: %v", len(got), tableNames(got))
	}
}

func TestApplyFilter_EmptyInput(t *testing.T) {
	e := makeEngine(map[string]bool{"a": true})
	got := e.applyFilter(nil)
	if len(got) != 0 {
		t.Errorf("empty input: want 0 tables, got %d", len(got))
	}
}

func TestApplyFilter_PreservesOrder(t *testing.T) {
	filter := map[string]bool{"a": true, "c": true}
	e := makeEngine(filter)
	tables := []config.Table{makeTable("a"), makeTable("b"), makeTable("c"), makeTable("d")}

	got := e.applyFilter(tables)
	if len(got) != 2 {
		t.Fatalf("want 2 tables, got %d", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("order not preserved: got %v", tableNames(got))
	}
}
