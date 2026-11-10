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

// openStep is one step of the open sequence, in the order openSequence runs
// them.
type openStep int

const (
	stepCwd openStep = iota
	stepInitialized
	stepConfig
	stepDatabaseFile
	stepDatabaseOpen
	stepDatabaseFormat
)

// openFailure names the step of the open sequence that failed. err is the
// cause as raised, before any command words it.
type openFailure struct {
	step openStep
	err  error
}

// at reports whether the sequence stopped at step. A nil failure is at no
// step.
func (f *openFailure) at(step openStep) bool { return f != nil && f.step == step }

// openSequence is the one path by which a command reads an existing graph:
// resolve cwd, verify .codegraph/ exists, load config, and open the SQLite
// database read-only, refusing a graph in another index format. It never
// creates, initializes or migrates anything. Queries reach it through
// openProject; health renders each step itself. A guard added here reaches
// both. The returned *db.Manager must be Closed by the caller on success.
func openSequence() (string, *config.Config, *db.Manager, *openFailure) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, nil, &openFailure{stepCwd, err}
	}
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, statErr := os.Stat(codegraphDir); os.IsNotExist(statErr) {
		return cwd, nil, nil, &openFailure{stepInitialized, fmt.Errorf("codegraph not initialized. Run 'codegraph init' first")}
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return cwd, nil, nil, &openFailure{stepConfig, err}
	}
	dbPath := cfg.GetDatabasePath(cwd)
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return cwd, cfg, nil, &openFailure{stepDatabaseFile, fmt.Errorf("database not found. Run 'codegraph build' first")}
	}
	dbm, err := db.OpenReadOnly(dbPath)
	if err != nil {
		return cwd, cfg, nil, &openFailure{stepDatabaseOpen, err}
	}
	if err := dbm.CheckFormat(); err != nil {
		dbm.Close()
		return cwd, cfg, nil, &openFailure{stepDatabaseFormat, err}
	}
	return cwd, cfg, dbm, nil
}

// openProject runs openSequence for a read-only query command and words its
// failure for the envelope: a missing file returns code "database_missing"
// and a graph in another index format returns "index_format_incompatible".
// On failure the stable error code can go straight into an EnvelopeError.
// The returned *db.Manager must be Closed by the caller on success.
func openProject() (string, *config.Config, *db.Manager, string, error) {
	cwd, cfg, dbm, failure := openSequence()
	if failure == nil {
		return cwd, cfg, dbm, "", nil
	}
	switch failure.step {
	case stepCwd:
		return cwd, cfg, nil, "cwd_failed", fmt.Errorf("failed to get current directory: %w", failure.err)
	case stepInitialized:
		return cwd, cfg, nil, "not_initialized", failure.err
	case stepConfig:
		return cwd, cfg, nil, "config_load_failed", fmt.Errorf("failed to load config: %w", failure.err)
	case stepDatabaseFile:
		return cwd, cfg, nil, "database_missing", failure.err
	case stepDatabaseFormat:
		var incompatible *db.IncompatibleFormatError
		if errors.As(failure.err, &incompatible) {
			return cwd, cfg, nil, "index_format_incompatible", failure.err
		}
	}
	return cwd, cfg, nil, "db_open_failed", failure.err
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
