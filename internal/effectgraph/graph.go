package effectgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"

type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
	Risk   string `json:"risk,omitempty"`
}
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}
type Graph struct {
	Version       string `json:"version"`
	TransactionID string `json:"transaction_id"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
	Digest        string `json:"digest"`
}

func Build(repo *ledger.Repository, txID string) (Graph, error) {
	if repo == nil || strings.TrimSpace(txID) == "" {
		return Graph{}, errors.New("repository and transaction id required")
	}
	s, err := repo.Snapshot(txID)
	if err != nil {
		return Graph{}, err
	}
	return FromSnapshot(s), nil
}
func FromSnapshot(s ledger.TransactionSnapshot) Graph {
	g := Graph{Version: Version, TransactionID: s.Transaction.ID}
	g.Nodes = append(g.Nodes, Node{ID: "transaction", Kind: "transaction", Label: s.Transaction.ID, Status: string(s.Transaction.Status)}, Node{ID: "repository", Kind: "repository", Label: "approved repository future"})
	g.Edges = append(g.Edges, Edge{From: "transaction", To: "repository", Kind: "contains"})
	if len(s.Rows["verification_runs"]) > 0 {
		g.Nodes = append(g.Nodes, Node{ID: "verification", Kind: "verification", Label: "deterministic verification", Status: latestString(s.Rows["verification_runs"], "outcome")})
		g.Edges = append(g.Edges, Edge{From: "repository", To: "verification", Kind: "verified_by"})
	}
	if len(s.Rows["approvals"]) > 0 {
		g.Nodes = append(g.Nodes, Node{ID: "approval", Kind: "approval", Label: "operator approval", Status: "approved"})
		from := "repository"
		if hasNode(g.Nodes, "verification") {
			from = "verification"
		}
		g.Edges = append(g.Edges, Edge{From: from, To: "approval", Kind: "authorizes"})
	}
	for _, e := range s.Effects {
		id := "effect:" + e.EffectID
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: "effect", Label: e.Operation + " @ " + e.Destination, Status: string(e.Status), Risk: e.RiskLevel})
		if len(e.DependsOn) == 0 {
			from := "repository"
			if hasNode(g.Nodes, "approval") {
				from = "approval"
			}
			g.Edges = append(g.Edges, Edge{From: from, To: id, Kind: "releases"})
		} else {
			for _, dep := range e.DependsOn {
				g.Edges = append(g.Edges, Edge{From: "effect:" + dep, To: id, Kind: "depends_on"})
			}
		}
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		if g.Edges[i].To != g.Edges[j].To {
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].Kind < g.Edges[j].Kind
	})
	g.Digest = digest(g)
	return g
}
func Mermaid(g Graph) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %s[\"%s\\n%s\"]\n", safe(n.ID), escape(n.Label), escape(n.Status))
	}
	for _, e := range g.Edges {
		fmt.Fprintf(&b, "  %s -- \"%s\" --> %s\n", safe(e.From), escape(e.Kind), safe(e.To))
	}
	return b.String()
}
func DOT(g Graph) string {
	var b strings.Builder
	b.WriteString("digraph FutureDiff {\n  rankdir=LR;\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %q [label=%q];\n", n.ID, n.Label+"\\n"+n.Status)
	}
	for _, e := range g.Edges {
		fmt.Fprintf(&b, "  %q -> %q [label=%q];\n", e.From, e.To, e.Kind)
	}
	b.WriteString("}\n")
	return b.String()
}
func digest(g Graph) string {
	g.Digest = ""
	b, _ := json.Marshal(g)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func hasNode(nodes []Node, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}
func latestString(rows []map[string]any, key string) string {
	if len(rows) == 0 {
		return ""
	}
	return anyString(rows[len(rows)-1][key])
}
func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\"", "'"), "[", "("), "]", ")")
}

var _ domain.ExternalEffect
