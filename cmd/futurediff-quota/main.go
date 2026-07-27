package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/quota"
)

func main() {
	root := flag.String("root", "", "FutureDiff data root")
	policyPath := flag.String("policy", "", "optional quota policy")
	transaction := flag.String("transaction", "", "optional transaction ID")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "--root is required")
		os.Exit(2)
	}
	policy := quota.Default()
	if *policyPath != "" {
		var err error
		policy, err = quota.Load(*policyPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer repo.Close()
	open, err := repo.CountOpenTransactions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result := map[string]any{"policy": policy, "open_transactions": open, "open_transactions_remaining": policy.MaxOpenTransactions - open}
	if *transaction != "" {
		effects, err := repo.CountEffects(*transaction)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		executions, err := repo.CountRuntimeExecutions(*transaction)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		result["transaction_id"] = *transaction
		result["effects"] = effects
		result["executions"] = executions
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
