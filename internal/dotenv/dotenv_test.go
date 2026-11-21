package dotenv

import (
	"reflect"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	input := `
# Comment
export API_KEY=%ENVCTL-OPENROUTER_KEY%
PLAIN=hello world # trailing comment
URL=https://example.com/path#anchor
DOUBLE="line1\nline2\"quoted\""
SINGLE='literal\n%ENVCTL-SINGLE_KEY%'
EMPTY=
`
	parsed := Parse(input)
	want := map[string]string{
		"API_KEY": "%ENVCTL-OPENROUTER_KEY%",
		"PLAIN":   "hello world",
		"URL":     "https://example.com/path#anchor",
		"DOUBLE":  "line1\nline2\"quoted\"",
		"SINGLE":  `literal\n%ENVCTL-SINGLE_KEY%`,
		"EMPTY":   "",
	}
	if !reflect.DeepEqual(parsed.Values, want) {
		t.Fatalf("Parse mismatch:\ngot:  %#v\nwant: %#v", parsed.Values, want)
	}
}

func TestResolvePrecedenceAndInlineSubstitution(t *testing.T) {
	parsed := Parse(`
API_KEY=%ENVCTL-OPENROUTER_KEY%
DB_URL=postgres://user:%ENVCTL-DB_PASS%@localhost:5432/%ENVCTL-DB_NAME%
PORT=3000
`)

	// When existing env already provides all placeholder vars, fetch must not be called.
	fetchCalled := false
	existing := map[string]string{
		"API_KEY": "prod-key",
		"DB_URL":  "postgres://prod",
	}
	resolved, missing, err := Resolve(parsed, existing, false, func(keys []string) (map[string]string, []string, error) {
		fetchCalled = true
		return nil, nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	if fetchCalled {
		t.Fatalf("expected fetch not to be called when all placeholders are shadowed by existing env")
	}
	if resolved["PORT"] != "3000" {
		t.Fatalf("expected PORT=3000, got %q", resolved["PORT"])
	}
	if _, exists := resolved["API_KEY"]; exists {
		t.Fatalf("did not expect shadowed API_KEY in resolved updates")
	}

	// With empty existing env, fetch is called once with sorted unique keys.
	var fetchedKeys []string
	resolved, missing, err = Resolve(parsed, map[string]string{}, false, func(keys []string) (map[string]string, []string, error) {
		fetchedKeys = keys
		return map[string]string{
			"OPENROUTER_KEY": "sk-or-123",
			"DB_PASS":        "s3cr3t",
			"DB_NAME":        "appdb",
		}, nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	wantKeys := []string{"DB_NAME", "DB_PASS", "OPENROUTER_KEY"}
	if !reflect.DeepEqual(fetchedKeys, wantKeys) {
		t.Fatalf("fetched keys mismatch: got %v want %v", fetchedKeys, wantKeys)
	}
	if resolved["API_KEY"] != "sk-or-123" {
		t.Fatalf("unexpected API_KEY: %q", resolved["API_KEY"])
	}
	if resolved["DB_URL"] != "postgres://user:s3cr3t@localhost:5432/appdb" {
		t.Fatalf("unexpected DB_URL: %q", resolved["DB_URL"])
	}
}
