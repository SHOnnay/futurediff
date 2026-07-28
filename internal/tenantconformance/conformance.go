package tenantconformance

import (
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}
type Report struct {
	Version    string  `json:"version"`
	Checks     []Check `json:"checks"`
	Passed     int     `json:"passed"`
	Failed     int     `json:"failed"`
	Conformant bool    `json:"conformant"`
}

func Run(dir string) (Report, error) {
	report := Report{Version: "1"}
	add := func(name string, ok bool, message string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: ok, Message: message})
		if ok {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	repo, err := ledger.OpenRepository(filepath.Join(dir, "ledger.db"))
	if err != nil {
		return report, err
	}
	defer repo.Close()
	create := func(id, owner string) (domain.Transaction, error) {
		return repo.Create(ledger.CreateInput{Transaction: domain.Transaction{ID: id, OwnerPrincipalID: owner, Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}, Workspace: domain.Workspace{TransactionID: id, RepositoryRoot: "/repo/" + id, GitCommonDir: "/repo/" + id + "/.git", BaseOID: "0123456789012345678901234567890123456789", ObjectFormat: "sha1", WorkspacePath: "/runtime/" + id, ArtifactsPath: "/runtime/" + id + "/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	}
	a, err := create("tx-tenant-a", "uid:1000")
	if err != nil {
		return report, err
	}
	_, err = create("tx-tenant-b", "uid:1001")
	if err != nil {
		return report, err
	}
	add("owner_persisted", a.OwnerPrincipalID == "uid:1000", "transaction owner must persist")
	ok, _ := repo.CheckTransactionAccess(a.ID, "uid:1000", ledger.AccessAdmin)
	add("owner_has_admin", ok, "owner must have implicit admin")
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessRead)
	add("stranger_denied", !ok, "unshared principal must be denied")
	err = repo.GrantTransactionAccess(a.ID, "uid:1000", "uid:1001", ledger.AccessRead, false, "req-read")
	add("owner_can_grant_read", err == nil, errorText(err))
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessRead)
	add("read_grant_allows_read", ok, "")
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessOperate)
	add("read_grant_blocks_operate", !ok, "")
	err = repo.GrantTransactionAccess(a.ID, "uid:1000", "uid:1001", ledger.AccessOperate, false, "req-operate")
	add("owner_can_upgrade_operate", err == nil, errorText(err))
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessOperate)
	add("operate_grant_allows_operate", ok, "")
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessAdmin)
	add("grants_never_allow_admin", !ok, "")
	err = repo.GrantTransactionAccess(a.ID, "uid:1002", "uid:1003", ledger.AccessRead, false, "")
	add("non_owner_cannot_grant", err != nil, "")
	own, _ := repo.ListTransactionsForPrincipal("uid:1000", false)
	shared, _ := repo.ListTransactionsForPrincipal("uid:1001", false)
	all, _ := repo.ListTransactionsForPrincipal("uid:2000", true)
	add("scoped_listing", len(own) == 1 && len(shared) == 2 && len(all) == 2, "owned/shared/all listing counts must differ")
	err = repo.RevokeTransactionAccess(a.ID, "uid:1000", "uid:1001", false, "req-revoke")
	ok, _ = repo.CheckTransactionAccess(a.ID, "uid:1001", ledger.AccessRead)
	add("revoke_removes_access", err == nil && !ok, errorText(err))
	head, err := repo.VerifyTransactionAccessChain()
	add("access_chain_valid", err == nil && head != "", errorText(err))
	report.Conformant = report.Failed == 0
	return report, nil
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
