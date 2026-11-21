package dotenv

import (
	"regexp"
	"sort"
	"strings"
)

var placeholderRe = regexp.MustCompile(`%ENVCTL-([A-Za-z0-9_./-]+)%`)

type Parsed struct {
	Values map[string]string
	Order  []string
}

func Parse(content string) Parsed {
	result := Parsed{
		Values: make(map[string]string),
	}
	lines := strings.Split(content, "\n")
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "export\t") {
			trimmed = strings.TrimSpace(trimmed[len("export"):])
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if key == "" {
			continue
		}
		rawVal := strings.TrimSpace(trimmed[eq+1:])
		val := parseValue(rawVal)

		if _, exists := result.Values[key]; !exists {
			result.Order = append(result.Order, key)
		}
		result.Values[key] = val
	}
	return result
}

func parseValue(raw string) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return parseDoubleQuoted(raw[1:])
	}
	if raw[0] == '\'' {
		if end := strings.IndexByte(raw[1:], '\''); end >= 0 {
			return raw[1 : 1+end]
		}
		return raw[1:]
	}
	// Strip inline comment only when '#' is preceded by whitespace.
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i > 0 && (raw[i-1] == ' ' || raw[i-1] == '\t') {
			return strings.TrimSpace(raw[:i])
		}
	}
	return raw
}

func parseDoubleQuoted(rest string) string {
	var b strings.Builder
	b.Grow(len(rest))
	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		if ch == '"' {
			return b.String()
		}
		if ch == '\\' && i+1 < len(rest) {
			i++
			switch rest[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte('\\')
				b.WriteByte(rest[i])
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func ExtractPlaceholders(value string) []string {
	matches := placeholderRe.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m[1]
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func HasPlaceholder(value string) bool {
	return placeholderRe.MatchString(value)
}

func ReplacePlaceholders(value string, secrets map[string]string) (string, []string) {
	var missing []string
	seenMissing := make(map[string]struct{})
	replaced := placeholderRe.ReplaceAllStringFunc(value, func(token string) string {
		m := placeholderRe.FindStringSubmatch(token)
		if len(m) < 2 {
			return token
		}
		name := m[1]
		if secret, ok := secrets[name]; ok {
			return secret
		}
		if _, dup := seenMissing[name]; !dup {
			seenMissing[name] = struct{}{}
			missing = append(missing, name)
		}
		return token
	})
	return replaced, missing
}

type FetchFunc func(keys []string) (values map[string]string, missing []string, err error)

// Resolve resolves parsed .env entries against existingEnv and fetches any
// referenced %ENVCTL-...% secrets in one batch.
func Resolve(parsed Parsed, existingEnv map[string]string, override bool, fetch FetchFunc) (map[string]string, []string, error) {
	resolved := make(map[string]string, len(parsed.Values))
	neededKeysSet := make(map[string]struct{})
	activeKeys := make([]string, 0, len(parsed.Order))

	for _, envKey := range parsed.Order {
		val := parsed.Values[envKey]
		if !override {
			if existing, ok := existingEnv[envKey]; ok && !HasPlaceholder(existing) {
				continue
			}
		}
		activeKeys = append(activeKeys, envKey)
		for _, secretName := range ExtractPlaceholders(val) {
			neededKeysSet[secretName] = struct{}{}
		}
	}

	var secrets map[string]string
	if len(neededKeysSet) > 0 {
		neededKeys := make([]string, 0, len(neededKeysSet))
		for k := range neededKeysSet {
			neededKeys = append(neededKeys, k)
		}
		sort.Strings(neededKeys)

		var missing []string
		var err error
		secrets, missing, err = fetch(neededKeys)
		if err != nil {
			return nil, nil, err
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, missing, nil
		}
	}

	for _, envKey := range activeKeys {
		val := parsed.Values[envKey]
		if len(secrets) > 0 && HasPlaceholder(val) {
			replaced, missing := ReplacePlaceholders(val, secrets)
			if len(missing) > 0 {
				sort.Strings(missing)
				return nil, missing, nil
			}
			resolved[envKey] = replaced
		} else {
			resolved[envKey] = val
		}
	}
	return resolved, nil, nil
}
