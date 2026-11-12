package db

import (
	"reflect"
	"testing"
)

func TestSearchSymbolsLiteral(t *testing.T) {
	m := newTestManager(t)
	for _, name := range []string{"plain", "has%percent", "has_under", `has\slash`, "has.dot", "has[bracket", "CaseName", "Éclair"} {
		seedSymbol(t, m, name, name, "source.go", 1)
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"%", []string{"has%percent"}},
		{"_", []string{"has_under"}},
		{`\`, []string{`has\slash`}},
		{".", []string{"has.dot"}},
		{"[", []string{"has[bracket"}},
		{"casename", []string{"CaseName"}}, // Preserve SQLite's existing ASCII folding.
		{"éclair", nil},                    // Do not invent Unicode case folding.
	} {
		t.Run(tc.query, func(t *testing.T) {
			rows, err := m.SearchSymbols(tc.query, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, row := range rows {
				names = append(names, row.Name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("SearchSymbols(%q)=%q, want %q", tc.query, names, tc.want)
			}
		})
	}
}
