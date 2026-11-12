package search

import (
	"context"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
)

func TestDatabaseExactSearchAppliesFilters(t *testing.T) {
	m, err := db.NewManager(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	for i, symbol := range []db.Symbol{
		{Name: "Run", Kind: "function", Language: "go"},
		{Name: "Run", Kind: "method", Language: "go"},
		{Name: "Run", Kind: "method", Language: "python"},
		{Name: "Run(arg)", Kind: "method", Language: "go"},
		{Name: "Owner.Run(arg)", Kind: "method", Language: "go"},
		{Name: "run", Kind: "method", Language: "go"},
		{Name: "Run", Kind: "module", Language: "go"},
	} {
		symbol.ID, symbol.File, symbol.Line = strconv.Itoa(i), "source", i+1
		if err := m.InsertSymbol(&symbol); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		kind  string
		langs []string
		want  []string
	}{
		{"", nil, []string{"0", "1", "2"}},
		{"method", []string{"go"}, []string{"1"}},
		{"method", []string{"python"}, []string{"2"}},
		{"class", nil, nil},
		{"module", []string{"go"}, []string{"6"}},
	} {
		t.Run(tc.kind+":"+strings.Join(tc.langs, ","), func(t *testing.T) {
			results, err := NewDatabaseTier(m).Search(context.Background(), SearchOptions{Query: "Run", ExactMatch: true, Kind: tc.kind, Languages: tc.langs})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, result := range results {
				ids = append(ids, result.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("ids=%v, want %v", ids, tc.want)
			}
		})
	}
}
