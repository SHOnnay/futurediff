package benchmark

import "testing"

func TestVerificationFailure(t *testing.T) {
	scenario := Scenario{FormatVersion: "0.1", ID: "fail", Steps: []Step{{ID: "code", Kind: "repository_mutation"}, {ID: "message", Kind: "external_effect", EffectKey: "slack"}, {ID: "verify", Kind: "verification", Fails: true}}}
	result, err := RunScenario(scenario)
	if err != nil {
		t.Fatal(err)
	}
	byMode := map[Mode]Metrics{}
	for _, m := range result.Metrics {
		byMode[m.Mode] = m
	}
	if byMode[FutureDiff].ReleasedEffects != 0 || byMode[FutureDiff].RepositoryChangedOnFail {
		t.Fatalf("FutureDiff released failed future: %+v", byMode[FutureDiff])
	}
	if byMode[Direct].UnsafeReleasedEffects != 1 || !byMode[Direct].RepositoryChangedOnFail {
		t.Fatalf("direct baseline mismatch: %+v", byMode[Direct])
	}
}

func TestDuplicateRetry(t *testing.T) {
	scenario := Scenario{FormatVersion: "0.1", ID: "retry", Steps: []Step{{ID: "effect", Kind: "external_effect", EffectKey: "issue", RetryAfterLoss: true}, {ID: "verify", Kind: "verification"}}}
	result, err := RunScenario(scenario)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range result.Metrics {
		if m.Mode == FutureDiff && m.DuplicateEffects != 0 {
			t.Fatal("FutureDiff duplicated effect")
		}
		if m.Mode == Direct && m.DuplicateEffects != 1 {
			t.Fatal("direct retry should duplicate")
		}
	}
}
