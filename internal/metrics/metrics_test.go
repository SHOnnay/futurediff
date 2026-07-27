package metrics

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"strings"
	"testing"
)

func TestPrometheusAggregateOnly(t *testing.T) {
	s := ledger.MetricsSnapshot{TransactionsTotal: 1, TransactionsByStatus: map[string]int64{"ready": 1}}
	v := Prometheus(s)
	if !strings.Contains(v, "futurediff_transactions_total 1") || strings.Contains(v, "transaction_id") {
		t.Fatalf("unexpected metrics %s", v)
	}
}
