package configattest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

func TestConfigurationAttestation(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "quota.json")
	if err := os.WriteFile(config, []byte(`{"version":"0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := operatorapproval.Generate("release-operator", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	env, err := Sign(priv, config, "quota_policy", time.Hour, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, env, config, "quota_policy", time.Unix(30, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"version":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, env, config, "quota_policy", time.Unix(30, 0)); err == nil {
		t.Fatal("expected drift rejection")
	}
}
