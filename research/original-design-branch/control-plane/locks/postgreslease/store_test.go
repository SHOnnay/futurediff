package postgreslease

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/futurediff/futurediff/internal/testpostgres"
)

func TestClaimRenewLoseAndReacquireLease(t *testing.T) {
	ctx := context.Background()
	db := startTestPostgres(t)
	store := New(db)
	if err := store.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap store: %v", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE transaction_state (transaction_id TEXT PRIMARY KEY, state TEXT NOT NULL)`); err != nil {
		t.Fatalf("create transaction_state: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO transaction_state (transaction_id, state) VALUES ($1, $2)`, "tx-spike", "COMMITTING"); err != nil {
		t.Fatalf("insert transaction state: %v", err)
	}

	claimed, err := store.Claim(ctx, "tx-spike", "worker-a", 250*time.Millisecond)
	if err != nil {
		t.Fatalf("claim worker-a: %v", err)
	}
	if !claimed {
		t.Fatal("expected worker-a to claim lease")
	}

	renewed, err := store.Renew(ctx, "tx-spike", "worker-a", 250*time.Millisecond)
	if err != nil {
		t.Fatalf("renew worker-a: %v", err)
	}
	if !renewed {
		t.Fatal("expected worker-a to renew lease")
	}

	claimed, err = store.Claim(ctx, "tx-spike", "worker-b", 250*time.Millisecond)
	if err != nil {
		t.Fatalf("claim worker-b before expiry: %v", err)
	}
	if claimed {
		t.Fatal("worker-b must not claim active lease")
	}

	time.Sleep(350 * time.Millisecond)

	claimed, err = store.Claim(ctx, "tx-spike", "worker-b", 250*time.Millisecond)
	if err != nil {
		t.Fatalf("claim worker-b after expiry: %v", err)
	}
	if !claimed {
		t.Fatal("expected worker-b to claim expired lease")
	}

	lease, err := store.Lookup(ctx, "tx-spike")
	if err != nil {
		t.Fatalf("lookup lease: %v", err)
	}
	if lease == nil || lease.OwnerID != "worker-b" {
		t.Fatalf("expected worker-b to own lease, got %#v", lease)
	}

	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM transaction_state WHERE transaction_id = $1`, "tx-spike").Scan(&state); err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	if state != "COMMITTING" {
		t.Fatalf("expected persisted state COMMITTING, got %s", state)
	}
}

func startTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	return testpostgres.Start(t).DB
}
