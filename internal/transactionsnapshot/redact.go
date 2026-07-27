package transactionsnapshot

import (
	"encoding/json"
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
}

func sanitizedJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	generic = redactValue("", generic)
	return json.MarshalIndent(generic, "", "  ")
}

func redactValue(key string, v any) any {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			if sensitiveKey(k) {
				out[k] = "[REDACTED]"
			} else {
				out[k] = redactValue(k, child)
			}
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = redactValue(key, child)
		}
		return out
	case string:
		if strings.HasSuffix(normalized, "_json") {
			var nested any
			if json.Unmarshal([]byte(value), &nested) == nil {
				encoded, _ := json.Marshal(redactValue("", nested))
				return string(encoded)
			}
		}
		result := value
		for _, pattern := range sensitivePatterns {
			result = pattern.ReplaceAllString(result, "[REDACTED]")
		}
		return result
	default:
		return value
	}
}
func sensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	if k == "credential_id" || strings.HasSuffix(k, "_digest") || strings.HasSuffix(k, "_sha256") {
		return false
	}
	for _, marker := range []string{"password", "secret", "authorization", "private_key", "api_key", "access_token", "refresh_token", "session_cookie", "source_reference"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}
