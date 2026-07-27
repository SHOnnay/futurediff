package apicontract

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type Change struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Endpoint string `json:"endpoint,omitempty"`
	Message  string `json:"message"`
}
type DiffReport struct {
	Compatible       bool     `json:"compatible"`
	BaselineVersion  string   `json:"baseline_version"`
	CandidateVersion string   `json:"candidate_version"`
	Changes          []Change `json:"changes,omitempty"`
}

func Diff(baseline, candidate Contract) DiffReport {
	report := DiffReport{Compatible: true, BaselineVersion: baseline.Version, CandidateVersion: candidate.Version}
	if major(baseline.Version) != major(candidate.Version) {
		report.Compatible = false
		report.Changes = append(report.Changes, Change{Severity: "breaking", Code: "major_version_changed", Message: fmt.Sprintf("major version changed from %s to %s", baseline.Version, candidate.Version)})
	}
	base := map[string]Endpoint{}
	cand := map[string]Endpoint{}
	for _, e := range baseline.Endpoints {
		base[key(e)] = e
	}
	for _, e := range candidate.Endpoints {
		cand[key(e)] = e
	}
	for k, b := range base {
		c, ok := cand[k]
		if !ok {
			report.Compatible = false
			report.Changes = append(report.Changes, Change{Severity: "breaking", Code: "endpoint_removed", Endpoint: k, Message: "endpoint was removed"})
			continue
		}
		if b.OperationID != c.OperationID {
			report.Compatible = false
			report.Changes = append(report.Changes, Change{Severity: "breaking", Code: "operation_id_changed", Endpoint: k, Message: fmt.Sprintf("operation id changed from %s to %s", b.OperationID, c.OperationID)})
		}
		if b.AgentSafe != c.AgentSafe {
			report.Compatible = false
			severity := "security"
			code := "agent_safety_changed"
			if b.AgentSafe && !c.AgentSafe {
				severity = "breaking"
				report.Compatible = false
			}
			report.Changes = append(report.Changes, Change{Severity: severity, Code: code, Endpoint: k, Message: fmt.Sprintf("agent_safe changed from %t to %t", b.AgentSafe, c.AgentSafe)})
		}
	}
	for k := range cand {
		if _, ok := base[k]; !ok {
			report.Changes = append(report.Changes, Change{Severity: "additive", Code: "endpoint_added", Endpoint: k, Message: "endpoint was added"})
		}
	}
	sort.Slice(report.Changes, func(i, j int) bool {
		if report.Changes[i].Severity != report.Changes[j].Severity {
			return report.Changes[i].Severity < report.Changes[j].Severity
		}
		if report.Changes[i].Endpoint != report.Changes[j].Endpoint {
			return report.Changes[i].Endpoint < report.Changes[j].Endpoint
		}
		return report.Changes[i].Code < report.Changes[j].Code
	})
	return report
}
func key(e Endpoint) string { return e.Method + " " + e.Path }
func major(v string) int    { part := strings.SplitN(v, ".", 2)[0]; n, _ := strconv.Atoi(part); return n }

// Validate checks structural uniqueness and, when present, the canonical digest.
func Validate(c Contract) error {
	if c.Version == "" || c.Transport == "" {
		return fmt.Errorf("contract version and transport are required")
	}
	endpointKeys := map[string]bool{}
	operations := map[string]bool{}
	endpoints := append([]Endpoint(nil), c.Endpoints...)
	for _, e := range endpoints {
		if e.Method == "" || e.Path == "" || e.OperationID == "" {
			return fmt.Errorf("endpoint method, path, and operation_id are required")
		}
		k := key(e)
		if endpointKeys[k] {
			return fmt.Errorf("duplicate endpoint %s", k)
		}
		endpointKeys[k] = true
		if operations[e.OperationID] {
			return fmt.Errorf("duplicate operation_id %s", e.OperationID)
		}
		operations[e.OperationID] = true
	}
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})
	digest, err := domain.Digest(map[string]any{"version": c.Version, "transport": c.Transport, "endpoints": endpoints})
	if err != nil {
		return err
	}
	if c.Digest != "" && c.Digest != digest {
		return fmt.Errorf("contract digest mismatch")
	}
	return nil
}
