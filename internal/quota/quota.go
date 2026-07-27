package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const Version = "0.1"

type Policy struct {
	Version                     string `json:"version"`
	MaxOpenTransactions         int64  `json:"max_open_transactions"`
	MaxEffectsPerTransaction    int64  `json:"max_effects_per_transaction"`
	MaxExecutionsPerTransaction int64  `json:"max_executions_per_transaction"`
	MaxPatchBytes               int64  `json:"max_patch_bytes"`
	MaxChangedPaths             int64  `json:"max_changed_paths"`
	MaxVerificationChecks       int64  `json:"max_verification_checks"`
}

func Default() Policy {
	return Policy{Version: Version, MaxOpenTransactions: 32, MaxEffectsPerTransaction: 32, MaxExecutionsPerTransaction: 128, MaxPatchBytes: 64 << 20, MaxChangedPaths: 5000, MaxVerificationChecks: 128}
}

func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported quota policy version %q", p.Version)
	}
	values := map[string]int64{"max_open_transactions": p.MaxOpenTransactions, "max_effects_per_transaction": p.MaxEffectsPerTransaction, "max_executions_per_transaction": p.MaxExecutionsPerTransaction, "max_patch_bytes": p.MaxPatchBytes, "max_changed_paths": p.MaxChangedPaths, "max_verification_checks": p.MaxVerificationChecks}
	for name, value := range values {
		if value <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}
	return nil
}

func Load(path string) (Policy, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Policy{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Policy{}, errors.New("quota policy must not be group/world writable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return Policy{}, errors.New("trailing JSON rejected")
	}
	return p, Validate(p)
}
