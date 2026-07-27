package openapispec

import (
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"testing"
)

func TestGenerateValidate(t *testing.T) {
	c := apicontract.Current()
	a := Generate(c)
	b := Generate(c)
	if a.Digest != b.Digest {
		t.Fatal("nondeterministic digest")
	}
	if err := Validate(a, c); err != nil {
		t.Fatal(err)
	}
	delete(a.Paths, "/v1/health")
	a.Digest = digest(a)
	if err := Validate(a, c); err == nil {
		t.Fatal("missing route accepted")
	}
}
