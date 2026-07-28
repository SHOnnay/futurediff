package apicontract

import "testing"

func TestMatch(t *testing.T) {
	m, ok := Match("POST", "/v1/transactions/tx-1/effects/e-1/refresh")
	if !ok || m.Endpoint.OperationID != "effect_refresh" || m.Params["id"] != "tx-1" || m.Params["effectID"] != "e-1" {
		t.Fatalf("unexpected match: %#v %t", m, ok)
	}
	if _, ok := Match("DELETE", "/v1/transactions/tx-1"); ok {
		t.Fatal("unexpected match")
	}
}
