package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"os"
	"strconv"
)

func main() {
	policyPath := flag.String("policy", "", "authorization policy JSON")
	uidText := flag.String("uid", "", "optional Unix UID to simulate")
	method := flag.String("method", "GET", "HTTP method")
	path := flag.String("path", "/v1/health", "API path")
	flag.Parse()
	if *policyPath == "" {
		fatal(fmt.Errorf("--policy is required"))
	}
	p, err := authorization.Load(*policyPath)
	if err != nil {
		fatal(err)
	}
	a, err := authorization.Compile(p)
	if err != nil {
		fatal(err)
	}
	out := map[string]any{"valid": true, "policy_digest": a.Digest(), "roles": len(p.Roles), "bindings": len(p.Bindings)}
	if *uidText != "" {
		v, err := strconv.ParseUint(*uidText, 10, 32)
		if err != nil {
			fatal(err)
		}
		m, ok := apicontract.Match(*method, *path)
		if !ok {
			fatal(fmt.Errorf("API route is not in the current contract"))
		}
		out["endpoint"] = m.Endpoint
		out["resource_id"] = m.Params["id"]
		out["decision"] = a.Decide(uint32(v), m.Endpoint.OperationID)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
