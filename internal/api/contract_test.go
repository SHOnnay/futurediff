package api

import (
	"encoding/json"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"net/http/httptest"
	"testing"
)

func TestContractEndpoint(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/contract", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var got apicontract.Contract
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Digest != apicontract.Current().Digest {
		t.Fatalf("digest mismatch")
	}
}
