package openapispec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/internal/apicontract"
)

const Version = "0.1"

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}
type Operation struct {
	OperationID string         `json:"operationId"`
	Summary     string         `json:"summary"`
	AgentSafe   bool           `json:"x-futurediff-agent-safe"`
	Responses   map[string]any `json:"responses"`
	RequestBody map[string]any `json:"requestBody,omitempty"`
}
type PathItem map[string]Operation
type Document struct {
	OpenAPI        string              `json:"openapi"`
	Info           Info                `json:"info"`
	Paths          map[string]PathItem `json:"paths"`
	ContractDigest string              `json:"x-futurediff-contract-digest"`
	SpecVersion    string              `json:"x-futurediff-spec-version"`
	Digest         string              `json:"x-futurediff-spec-digest"`
}

func Generate(c apicontract.Contract) Document {
	d := Document{OpenAPI: "3.1.0", Info: Info{Title: "FutureDiff Local API", Version: c.Version, Description: "Private Unix-socket HTTP API. Agent-safe authority is declared per operation."}, Paths: map[string]PathItem{}, ContractDigest: c.Digest, SpecVersion: Version}
	for _, e := range c.Endpoints {
		item := d.Paths[e.Path]
		if item == nil {
			item = PathItem{}
		}
		op := Operation{OperationID: e.OperationID, Summary: strings.ReplaceAll(e.OperationID, "_", " "), AgentSafe: e.AgentSafe, Responses: map[string]any{"200": map[string]any{"description": "Successful response"}, "4XX": map[string]any{"description": "Request rejected"}, "5XX": map[string]any{"description": "Server or policy failure"}}}
		if e.Method != "GET" && e.Method != "HEAD" {
			op.RequestBody = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}}
		}
		item[strings.ToLower(e.Method)] = op
		d.Paths[e.Path] = item
	}
	d.Digest = digest(d)
	return d
}
func digest(d Document) string {
	d.Digest = ""
	b, _ := json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func Validate(d Document, c apicontract.Contract) error {
	if d.OpenAPI != "3.1.0" {
		return fmt.Errorf("unsupported OpenAPI version %q", d.OpenAPI)
	}
	if d.SpecVersion != Version {
		return fmt.Errorf("unsupported FutureDiff spec version %q", d.SpecVersion)
	}
	if d.ContractDigest != c.Digest {
		return errors.New("OpenAPI contract digest does not match current API contract")
	}
	if d.Digest == "" || d.Digest != digest(d) {
		return errors.New("OpenAPI document digest mismatch")
	}
	expected := map[string]apicontract.Endpoint{}
	for _, e := range c.Endpoints {
		expected[e.Method+" "+e.Path] = e
	}
	seen := map[string]bool{}
	for path, item := range d.Paths {
		for method, op := range item {
			key := strings.ToUpper(method) + " " + path
			e, ok := expected[key]
			if !ok {
				return fmt.Errorf("unexpected OpenAPI operation %s", key)
			}
			if seen[key] {
				return fmt.Errorf("duplicate OpenAPI operation %s", key)
			}
			seen[key] = true
			if op.OperationID != e.OperationID {
				return fmt.Errorf("operation ID mismatch for %s", key)
			}
			if op.AgentSafe != e.AgentSafe {
				return fmt.Errorf("agent-safe mismatch for %s", key)
			}
		}
	}
	missing := []string{}
	for key := range expected {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("OpenAPI operations missing: %s", strings.Join(missing, ", "))
	}
	return nil
}
func Load(path string) (Document, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Document{}, e
	}
	var d Document
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&d); e != nil {
		return d, e
	}
	var extra any
	if e = dec.Decode(&extra); e == nil {
		return d, errors.New("trailing JSON data")
	} else if !errors.Is(e, io.EOF) {
		return d, e
	}
	return d, nil
}
