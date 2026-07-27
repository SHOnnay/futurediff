package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"os"
)

func main() {
	socket := flag.String("socket", "", "optional daemon Unix socket to compare")
	output := flag.String("output", "", "optional output path")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	local := apicontract.Current()
	result := map[string]any{"local": local, "compatible": true}
	if *socket != "" {
		raw, e := api.NewClient(*socket).Do("GET", "/v1/contract", nil)
		if e != nil {
			fail(e)
		}
		var remote apicontract.Contract
		if e := json.Unmarshal(raw, &remote); e != nil {
			fail(e)
		}
		result["remote"] = remote
		result["compatible"] = remote.Digest == local.Digest
		if remote.Digest != local.Digest {
			result["reason"] = "daemon API contract digest differs from this client"
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	if *output != "" {
		if e := os.WriteFile(*output, append(b, '\n'), 0o600); e != nil {
			fail(e)
		}
	}
	fmt.Println(string(b))
	if ok, _ := result["compatible"].(bool); !ok {
		os.Exit(2)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
