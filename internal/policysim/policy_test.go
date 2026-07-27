package policysim

import (
	"github.com/SHOnnay/futurediff/internal/verification"
	"testing"
)

func TestExplainSimulation(t *testing.T) {
	c := verification.Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "a", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "a"}, {CheckID: "b", Required: true, DependsOn: []string{"a"}, Executor: "oci_command", Type: "command", Command: []string{"true"}, TimeoutSeconds: 10}}}
	r, e := Explain(c, map[string]string{"a": "fail", "b": "pass"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if r.Outcome != "fail" || r.Checks[1].Simulated != "blocked" {
		t.Fatalf("unexpected %+v", r)
	}
}
