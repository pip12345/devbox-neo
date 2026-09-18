package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
)

// Decoder messages can contain literal numbers or invalid characters from
// private config values. Report only field names, structural types and offsets;
// unknown error text must not become part of the saved report or journal.
func schemaDiagnostic(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("malformed JSON at byte %d (values withheld)", syntax.Offset)
	}
	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &mismatch) {
		actual := "incompatible JSON value"
		for _, kind := range []string{"string", "number", "bool", "object", "array", "null"} {
			if mismatch.Value == kind || strings.HasPrefix(mismatch.Value, kind+" ") {
				actual = kind
				break
			}
		}
		return fmt.Sprintf("field %s: expected %s, got %s at byte %d (values withheld)", strconv.QuoteToASCII(mismatch.Field), jsonType(mismatch.Type), actual, mismatch.Offset)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "incomplete JSON document (values withheld)"
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		message := cause.Error()
		if field, ok := strings.CutPrefix(message, "json: unknown field "); ok {
			if name, e := strconv.Unquote(field); e == nil {
				return "unknown field " + strconv.QuoteToASCII(name) + " (value withheld)"
			}
		}
		switch message {
		case "expected a JSON object", "expected exactly one JSON value", "duplicate JSON field", "invalid object key", "unexpected JSON delimiter":
			return message + " (values withheld)"
		}
	}
	return "invalid schema (details withheld)"
}

func jsonType(t reflect.Type) string {
	if t == nil {
		return "the declared JSON type"
	}
	switch t.Kind() {
	case reflect.Pointer:
		return jsonType(t.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "the declared JSON type"
	}
}

// These helpers inspect presence only after the strict source decoder succeeds.
// Missing, null and numeric zero have distinct diagnostics, not new defaults.
func fieldPresence(data []byte, field string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return "unreadable"
	}
	raw, exists := fields[field]
	if !exists {
		return "missing"
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "null"
	}
	return "present"
}
func versionDiagnostic(data []byte, field string, actual, expected int) string {
	if actual == expected {
		return ""
	}
	found := fieldPresence(data, field)
	if found == "present" {
		found = strconv.Itoa(actual)
	}
	return fmt.Sprintf("field %s: found %s; expected version %d", strconv.QuoteToASCII(field), found, expected)
}

// Filesystem failures have no config payload. Keep OS error categories without
// echoing arbitrary wrapped error text; callers supply the relevant public path.
func filesystemDiagnostic(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "path or symlink target does not exist"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	case errors.Is(err, syscall.ENOTDIR):
		return "a path component is not a directory"
	case errors.Is(err, syscall.ELOOP):
		return "too many symbolic links (possible loop)"
	}
	if err.Error() == "EvalSymlinks: too many links" {
		return "too many symbolic links (possible loop)"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	if strings.HasPrefix(err.Error(), "symlink component is not supported:") {
		return "path contains a symlink component; this reader requires direct host paths"
	}
	if strings.HasPrefix(err.Error(), "expected a regular file:") {
		return "expected a regular file"
	}
	return "filesystem inspection failed (details withheld)"
}

func workspaceDiagnostic(path string) string {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Sprintf("Cannot resolve workspace %s: %s.", strconv.QuoteToASCII(path), filesystemDiagnostic(err))
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("Cannot inspect workspace %s: %s.", strconv.QuoteToASCII(path), filesystemDiagnostic(err))
	}
	if !info.IsDir() {
		return "Workspace is not a directory: " + strconv.QuoteToASCII(path) + "."
	}
	if canonical != path {
		return fmt.Sprintf("Workspace path is not canonical: %s resolves to %s.", strconv.QuoteToASCII(path), strconv.QuoteToASCII(canonical))
	}
	return ""
}
