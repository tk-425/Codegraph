package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// openProject runs the common scaffolding shared by every read-only query
// command: resolve cwd, verify .codegraph/ exists, load config, and open the
// existing SQLite database read-only. Queries never create, initialize or
// migrate a database; a missing file returns code "database_missing" and a
// graph in another index format returns "index_format_incompatible". On
// failure the stable error code can go straight into an EnvelopeError. The
// returned *db.Manager must be Closed by the caller on success.
func openProject() (string, *config.Config, *db.Manager, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, nil, "cwd_failed", fmt.Errorf("failed to get current directory: %w", err)
	}
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, statErr := os.Stat(codegraphDir); os.IsNotExist(statErr) {
		return cwd, nil, nil, "not_initialized", fmt.Errorf("codegraph not initialized. Run 'codegraph init' first")
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return cwd, nil, nil, "config_load_failed", fmt.Errorf("failed to load config: %w", err)
	}
	dbPath := cfg.GetDatabasePath(cwd)
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return cwd, cfg, nil, "database_missing", fmt.Errorf("database not found. Run 'codegraph build' first")
	}
	dbm, err := db.OpenReadOnly(dbPath)
	if err != nil {
		return cwd, cfg, nil, "db_open_failed", err
	}
	if err := dbm.CheckFormat(); err != nil {
		dbm.Close()
		var incompatible *db.IncompatibleFormatError
		if errors.As(err, &incompatible) {
			return cwd, cfg, nil, "index_format_incompatible", err
		}
		return cwd, cfg, nil, "db_open_failed", err
	}
	return cwd, cfg, dbm, "", nil
}

// jsonOutputFlag is set by the persistent --json root flag. When true,
// in-scope read-only query commands emit a single JSON envelope to stdout
// instead of their human-formatted output.
var jsonOutputFlag bool

// JSONOutputEnabled reports whether the --json flag is currently set.
func JSONOutputEnabled() bool { return jsonOutputFlag }

// Envelope is the uniform JSON output contract for query commands.
// Every key is always present. Query is null for commands that take no
// query argument. Count always equals the length of Results.
type Envelope struct {
	Command string          `json:"command"`
	Query   *string         `json:"query"`
	Count   int             `json:"count"`
	Results any             `json:"results"`
	Errors  []EnvelopeError `json:"errors"`
}

// EnvelopeError is one entry in the envelope's errors array.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EmitJSON writes a single Envelope to w. The count field is derived from
// the length of results via reflection when results is a slice or array;
// a nil results value becomes an empty JSON array with count 0. A nil errs
// slice becomes an empty JSON array.
func EmitJSON(w io.Writer, command string, query *string, results any, errs []EnvelopeError) error {
	count := 0
	if results != nil {
		rv := reflect.ValueOf(results)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			count = rv.Len()
		}
	}
	if results == nil {
		results = []any{}
	}
	if errs == nil {
		errs = []EnvelopeError{}
	}
	env := Envelope{
		Command: command,
		Query:   query,
		Count:   count,
		Results: results,
		Errors:  errs,
	}
	return json.NewEncoder(w).Encode(env)
}
