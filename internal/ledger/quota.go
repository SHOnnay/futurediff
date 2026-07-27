package ledger

func (r *Repository) CountOpenTransactions() (int64, error) {
	row, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM transactions WHERE status NOT IN ('committed','aborted','compensated','manual_intervention')`)
	if err != nil {
		return 0, err
	}
	return Int64(row, "n"), nil
}
func (r *Repository) CountEffects(transactionID string) (int64, error) {
	row, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM effects WHERE transaction_id=? AND status <> 'superseded'`, transactionID)
	if err != nil {
		return 0, err
	}
	return Int64(row, "n"), nil
}
func (r *Repository) CountRuntimeExecutions(transactionID string) (int64, error) {
	row, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM runtime_executions WHERE transaction_id=?`, transactionID)
	if err != nil {
		return 0, err
	}
	return Int64(row, "n"), nil
}
