package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEmitJSON(t *testing.T) {
	type rec struct {
		Name string `json:"name"`
	}

	mustKeys := []string{"command", "query", "count", "results", "errors"}

	hasAllKeys := func(t *testing.T, raw map[string]json.RawMessage) {
		t.Helper()
		for _, k := range mustKeys {
			if _, ok := raw[k]; !ok {
				t.Fatalf("missing required key %q in envelope: %v", k, raw)
			}
		}
	}

	q := "needle"

	cases := []struct {
		name      string
		command   string
		query     *string
		results   any
		errs      []EnvelopeError
		wantCount int
		wantQuery string // raw JSON form: "null" or `"needle"`
		wantErrs  string // raw JSON form
	}{
		{
			name:      "non-empty results with query",
			command:   "search",
			query:     &q,
			results:   []rec{{Name: "a"}, {Name: "b"}},
			errs:      nil,
			wantCount: 2,
			wantQuery: `"needle"`,
			wantErrs:  `[]`,
		},
		{
			name:      "empty slice results",
			command:   "search",
			query:     &q,
			results:   []rec{},
			errs:      nil,
			wantCount: 0,
			wantQuery: `"needle"`,
			wantErrs:  `[]`,
		},
		{
			name:      "nil results",
			command:   "health",
			query:     nil,
			results:   nil,
			errs:      nil,
			wantCount: 0,
			wantQuery: `null`,
			wantErrs:  `[]`,
		},
		{
			name:      "populated errors",
			command:   "search",
			query:     &q,
			results:   []rec{},
			errs:      []EnvelopeError{{Code: "search_failed", Message: "boom"}},
			wantCount: 0,
			wantQuery: `"needle"`,
			wantErrs:  `[{"code":"search_failed","message":"boom"}]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := EmitJSON(&buf, tc.command, tc.query, tc.results, tc.errs); err != nil {
				t.Fatalf("EmitJSON returned error: %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
				t.Fatalf("output is not valid JSON: %v\nraw=%s", err, buf.String())
			}
			hasAllKeys(t, raw)

			var cmd string
			if err := json.Unmarshal(raw["command"], &cmd); err != nil {
				t.Fatalf("command unmarshal: %v", err)
			}
			if cmd != tc.command {
				t.Errorf("command = %q, want %q", cmd, tc.command)
			}

			var count int
			if err := json.Unmarshal(raw["count"], &count); err != nil {
				t.Fatalf("count unmarshal: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}

			if string(raw["query"]) != tc.wantQuery {
				t.Errorf("query raw = %s, want %s", string(raw["query"]), tc.wantQuery)
			}

			if string(raw["errors"]) != tc.wantErrs {
				t.Errorf("errors raw = %s, want %s", string(raw["errors"]), tc.wantErrs)
			}

			// results must always be a JSON array, never null
			if len(raw["results"]) == 0 || raw["results"][0] != '[' {
				t.Errorf("results should be a JSON array, got %s", string(raw["results"]))
			}
		})
	}
}

func TestEmitJSON_CountMatchesLen(t *testing.T) {
	type rec struct{ N int }
	results := []rec{{1}, {2}, {3}, {4}, {5}}

	var buf bytes.Buffer
	if err := EmitJSON(&buf, "callers", nil, results, nil); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	var env struct {
		Count   int   `json:"count"`
		Results []rec `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Count != len(env.Results) {
		t.Errorf("count=%d, len(results)=%d", env.Count, len(env.Results))
	}
	if env.Count != 5 {
		t.Errorf("count=%d, want 5", env.Count)
	}
}
