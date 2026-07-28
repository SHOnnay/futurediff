package apicontract

import "strings"

type MatchResult struct {
	Endpoint Endpoint          `json:"endpoint"`
	Params   map[string]string `json:"params,omitempty"`
}

func Match(method, requestPath string) (MatchResult, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	pathParts := splitPath(requestPath)
	for _, endpoint := range Current().Endpoints {
		if endpoint.Method != method {
			continue
		}
		patternParts := splitPath(endpoint.Path)
		if len(patternParts) != len(pathParts) {
			continue
		}
		params := map[string]string{}
		matched := true
		for i := range patternParts {
			part := patternParts[i]
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
				if name == "" || pathParts[i] == "" {
					matched = false
					break
				}
				params[name] = pathParts[i]
				continue
			}
			if part != pathParts[i] {
				matched = false
				break
			}
		}
		if matched {
			return MatchResult{Endpoint: endpoint, Params: params}, true
		}
	}
	return MatchResult{}, false
}

func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	if q := strings.IndexByte(path, '?'); q >= 0 {
		path = path[:q]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
