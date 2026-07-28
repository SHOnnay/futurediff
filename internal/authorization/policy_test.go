package authorization

import "testing"

func TestPolicyDenyByDefaultAndAgentSafety(t *testing.T) {
	a, err := Compile(Policy{Version: Version, Default: "deny", AgentRoles: []string{"agent"}, Roles: []Role{{Name: "agent", Operations: []string{"health", "transaction_create"}}, {Name: "operator", Operations: []string{"transaction_commit"}}}, Bindings: []Binding{{UID: 1000, Roles: []string{"agent"}}, {UID: 2000, Roles: []string{"operator"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Decide(1000, "health").Allowed || a.Decide(1000, "transaction_commit").Allowed || !a.Decide(2000, "transaction_commit").Allowed {
		t.Fatal("unexpected decision")
	}
	if _, err := Compile(Policy{Version: Version, Default: "deny", AgentRoles: []string{"agent"}, Roles: []Role{{Name: "agent", Operations: []string{"transaction_commit"}}}}); err == nil {
		t.Fatal("expected unsafe agent role rejection")
	}
}
