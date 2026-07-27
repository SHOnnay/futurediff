package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/openapispec"
	"os"
)

func main() {
	socket := flag.String("socket", "", "optional daemon socket")
	verify := flag.String("verify", "", "optional OpenAPI JSON to verify")
	output := flag.String("output", "", "optional output path")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	c := apicontract.Current()
	doc := openapispec.Generate(c)
	result := map[string]any{"document": doc, "valid": true}
	if *verify != "" {
		loaded, e := openapispec.Load(*verify)
		if e != nil {
			fail(e)
		}
		e = openapispec.Validate(loaded, c)
		result["verified_file"] = *verify
		result["valid"] = e == nil
		if e != nil {
			result["error"] = e.Error()
		}
	}
	if *socket != "" {
		raw, e := api.NewClient(*socket).Do("GET", "/v1/openapi", nil)
		if e != nil {
			fail(e)
		}
		var remote openapispec.Document
		if e = json.Unmarshal(raw, &remote); e != nil {
			fail(e)
		}
		e = openapispec.Validate(remote, c)
		result["remote"] = remote
		result["remote_valid"] = e == nil && remote.Digest == doc.Digest
		if e != nil {
			result["remote_error"] = e.Error()
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	if *output != "" {
		if e := os.WriteFile(*output, append(b, '\n'), 0o600); e != nil {
			fail(e)
		}
	}
	fmt.Println(string(b))
	if ok, _ := result["valid"].(bool); !ok {
		os.Exit(2)
	}
	if ok, exists := result["remote_valid"].(bool); exists && !ok {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
