package transactionsnapshot

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/futurepack"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type Report struct {
	TransactionID string `json:"transaction_id"`
	Output        string `json:"output"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArtifactCount int    `json:"artifact_count"`
}

func Export(repo *ledger.Repository, transactionID, output string) (Report, error) {
	if repo == nil || transactionID == "" || output == "" {
		return Report{}, errors.New("repository, transaction id, and output are required")
	}
	snapshot, err := repo.Snapshot(transactionID)
	if err != nil {
		return Report{}, err
	}
	temp, err := os.MkdirTemp("", "futurediff-export-*")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(temp)
	store, err := futurepack.Open(temp)
	if err != nil {
		return Report{}, err
	}
	encoded, err := sanitizedJSON(snapshot)
	if err != nil {
		return Report{}, err
	}
	refs := []futurepack.Ref{}
	ref, err := store.PutBytes("transaction-snapshot.json", append(encoded, '\n'))
	if err != nil {
		return Report{}, err
	}
	refs = append(refs, ref)
	if snapshot.Patch != nil {
		if info, statErr := os.Stat(snapshot.Patch.PatchPath); statErr == nil && info.Mode().IsRegular() {
			patchRef, putErr := store.PutFile(filepath.Base(snapshot.Patch.PatchPath), snapshot.Patch.PatchPath)
			if putErr != nil {
				return Report{}, putErr
			}
			refs = append(refs, patchRef)
		}
	}
	manifest := futurepack.Manifest{FormatVersion: "0.2", TransactionID: transactionID, Scenario: "transaction-forensic-export", Verdict: string(snapshot.Transaction.Status), Metadata: map[string]any{"material_revision": snapshot.Transaction.MaterialRevision, "revision": snapshot.Transaction.Revision}, Artifacts: refs}
	if err := store.Export(output, manifest); err != nil {
		return Report{}, err
	}
	if _, err := futurepack.VerifyArchive(output); err != nil {
		return Report{}, err
	}
	digest, err := fileSHA256(output)
	if err != nil {
		return Report{}, err
	}
	return Report{TransactionID: transactionID, Output: output, ArchiveSHA256: digest, ArtifactCount: len(refs)}, nil
}
