package authzconformance

import (
	"github.com/SHOnnay/futurediff/internal/authorization"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	p := authorization.Policy{Version: authorization.Version, Default: "deny", AgentRoles: []string{"agent"}, Roles: []authorization.Role{{Name: "agent", Operations: []string{"health"}}, {Name: "operator", Operations: []string{"transaction_commit"}}}, Bindings: []authorization.Binding{{UID: 1000, Roles: []string{"agent"}}, {UID: 2000, Roles: []string{"operator"}}}}
	r := Run(p, t.TempDir(), time.Now())
	if !r.Conformant {
		t.Fatalf("%+v", r)
	}
}
