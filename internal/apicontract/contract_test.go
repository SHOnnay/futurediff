package apicontract

import "testing"

func TestCurrentContractIsStableAndSeparatesAuthority(t *testing.T) {
	c := Current()
	if len(c.Endpoints) < 10 || len(c.Digest) != 64 {
		t.Fatalf("invalid contract: %+v", c)
	}
	for _, e := range c.Endpoints {
		if e.OperationID == "transaction_approve" || e.OperationID == "transaction_commit" {
			if e.AgentSafe {
				t.Fatalf("privileged endpoint marked agent-safe: %+v", e)
			}
		}
	}
}

func TestDiffDetectsBreakingAndAdditiveChanges(t *testing.T) {
	base := Current()
	candidate := base
	candidate.Endpoints = append([]Endpoint(nil), base.Endpoints...)
	candidate.Endpoints = candidate.Endpoints[1:]
	report := Diff(base, candidate)
	if report.Compatible {
		t.Fatal("expected removed endpoint to be breaking")
	}
	additive := base
	additive.Endpoints = append(append([]Endpoint(nil), base.Endpoints...), Endpoint{Method: "GET", Path: "/v1/new", OperationID: "new", AgentSafe: true})
	if report := Diff(base, additive); !report.Compatible {
		t.Fatalf("additive change should be compatible: %+v", report)
	}
}

func TestValidateRejectsDigestMismatchAndDuplicates(t *testing.T) {
	c := Current()
	c.Digest = "bad"
	if err := Validate(c); err == nil {
		t.Fatal("expected digest mismatch")
	}
	c = Current()
	c.Endpoints = append(c.Endpoints, c.Endpoints[0])
	c.Digest = ""
	if err := Validate(c); err == nil {
		t.Fatal("expected duplicate endpoint")
	}
}
func TestDiffTreatsAgentSafetyChangeAsIncompatible(t *testing.T) {
	base := Current()
	candidate := base
	candidate.Endpoints = append([]Endpoint(nil), base.Endpoints...)
	candidate.Endpoints[0].AgentSafe = !candidate.Endpoints[0].AgentSafe
	candidate.Digest = ""
	if report := Diff(base, candidate); report.Compatible {
		t.Fatal("agent safety change must be incompatible")
	}
}
