package authorization

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

const Version = "1"

type Role struct {
	Name          string   `json:"name"`
	Operations    []string `json:"operations"`
	ResourceScope string   `json:"resource_scope,omitempty"`
}

type Binding struct {
	UID   uint32   `json:"uid"`
	Roles []string `json:"roles"`
}

type Policy struct {
	Version    string    `json:"version"`
	Default    string    `json:"default"`
	AgentRoles []string  `json:"agent_roles,omitempty"`
	Roles      []Role    `json:"roles"`
	Bindings   []Binding `json:"bindings"`
}

type Decision struct {
	Allowed       bool     `json:"allowed"`
	UID           uint32   `json:"uid"`
	OperationID   string   `json:"operation_id"`
	Roles         []string `json:"roles,omitempty"`
	ResourceScope string   `json:"resource_scope,omitempty"`
	ReasonCode    string   `json:"reason_code"`
	PolicyDigest  string   `json:"policy_digest"`
}

type Authorizer struct {
	policy     Policy
	digest     string
	roleOps    map[string]map[string]struct{}
	roleScopes map[string]string
	bindings   map[uint32][]string
	agentRoles map[string]struct{}
	allOps     map[string]apicontract.Endpoint
}

func Load(path string) (Policy, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Policy{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Policy{}, fmt.Errorf("authorization policy must not be group/world writable: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return Policy{}, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return Policy{}, err
	}
	if _, err := Compile(p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func Compile(p Policy) (*Authorizer, error) {
	if p.Version != Version {
		return nil, fmt.Errorf("unsupported authorization policy version %q", p.Version)
	}
	if p.Default == "" {
		p.Default = "deny"
	}
	if p.Default != "deny" {
		return nil, errors.New("authorization policy default must be deny")
	}
	contract := apicontract.Current()
	allOps := map[string]apicontract.Endpoint{}
	for _, e := range contract.Endpoints {
		allOps[e.OperationID] = e
	}
	roleOps := map[string]map[string]struct{}{}
	roleScopes := map[string]string{}
	for i, role := range p.Roles {
		name := strings.TrimSpace(role.Name)
		if name == "" || roleOps[name] != nil {
			return nil, errors.New("role names must be non-empty and unique")
		}
		if len(role.Operations) == 0 {
			return nil, fmt.Errorf("role %s has no operations", name)
		}
		scope := strings.TrimSpace(role.ResourceScope)
		if scope == "" {
			scope = "owned"
		}
		if scope != "owned" && scope != "all" {
			return nil, fmt.Errorf("role %s has invalid resource_scope %q", name, scope)
		}
		p.Roles[i].Name = name
		p.Roles[i].ResourceScope = scope
		roleScopes[name] = scope
		set := map[string]struct{}{}
		for _, op := range role.Operations {
			op = strings.TrimSpace(op)
			if op == "*" {
				for id := range allOps {
					set[id] = struct{}{}
				}
				continue
			}
			if _, ok := allOps[op]; !ok {
				return nil, fmt.Errorf("role %s references unknown operation %s", name, op)
			}
			if _, duplicate := set[op]; duplicate {
				return nil, fmt.Errorf("role %s repeats operation %s", name, op)
			}
			set[op] = struct{}{}
		}
		roleOps[name] = set
	}
	bindings := map[uint32][]string{}
	for _, b := range p.Bindings {
		if _, duplicate := bindings[b.UID]; duplicate {
			return nil, fmt.Errorf("duplicate binding for uid %d", b.UID)
		}
		if len(b.Roles) == 0 {
			return nil, fmt.Errorf("uid %d has no roles", b.UID)
		}
		seen := map[string]bool{}
		for _, role := range b.Roles {
			if roleOps[role] == nil {
				return nil, fmt.Errorf("uid %d references unknown role %s", b.UID, role)
			}
			if seen[role] {
				return nil, fmt.Errorf("uid %d repeats role %s", b.UID, role)
			}
			seen[role] = true
		}
		bindings[b.UID] = append([]string(nil), b.Roles...)
		sort.Strings(bindings[b.UID])
	}
	agentRoles := map[string]struct{}{}
	for _, role := range p.AgentRoles {
		if roleOps[role] == nil {
			return nil, fmt.Errorf("unknown agent role %s", role)
		}
		agentRoles[role] = struct{}{}
		if roleScopes[role] != "owned" {
			return nil, fmt.Errorf("agent role %s must use owned resource scope", role)
		}
		for op := range roleOps[role] {
			if !allOps[op].AgentSafe {
				return nil, fmt.Errorf("agent role %s grants unsafe operation %s", role, op)
			}
		}
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	return &Authorizer{policy: p, digest: hex.EncodeToString(sum[:]), roleOps: roleOps, roleScopes: roleScopes, bindings: bindings, agentRoles: agentRoles, allOps: allOps}, nil
}

func (a *Authorizer) Digest() string {
	if a == nil {
		return ""
	}
	return a.digest
}
func (a *Authorizer) Policy() Policy { return a.policy }
func (a *Authorizer) Decide(uid uint32, operationID string) Decision {
	d := Decision{UID: uid, OperationID: operationID, PolicyDigest: a.digest, ReasonCode: "default_deny"}
	if _, known := a.allOps[operationID]; !known {
		d.ReasonCode = "unknown_operation"
		return d
	}
	roles := append([]string(nil), a.bindings[uid]...)
	d.Roles = roles
	if len(roles) == 0 {
		d.ReasonCode = "no_binding"
		return d
	}
	granted := false
	scope := "owned"
	for _, role := range roles {
		if _, ok := a.roleOps[role][operationID]; ok {
			granted = true
			if a.roleScopes[role] == "all" {
				scope = "all"
			}
		}
	}
	if granted {
		d.Allowed = true
		d.ResourceScope = scope
		d.ReasonCode = "role_grant"
	}
	return d
}

func (a *Authorizer) IsAgentRole(role string) bool { _, ok := a.agentRoles[role]; return ok }
