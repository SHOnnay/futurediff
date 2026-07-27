package configattest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

const Version = "0.1"

type Envelope struct {
	Version    string    `json:"version"`
	Kind       string    `json:"kind"`
	FileSHA256 string    `json:"file_sha256"`
	FileSize   int64     `json:"file_size"`
	Approver   string    `json:"approver"`
	KeyID      string    `json:"key_id"`
	SignedAt   time.Time `json:"signed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Nonce      string    `json:"nonce"`
	Signature  string    `json:"signature"`
}

type payload struct {
	Version    string    `json:"version"`
	Kind       string    `json:"kind"`
	FileSHA256 string    `json:"file_sha256"`
	FileSize   int64     `json:"file_size"`
	Approver   string    `json:"approver"`
	KeyID      string    `json:"key_id"`
	SignedAt   time.Time `json:"signed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Nonce      string    `json:"nonce"`
}

func SidecarPath(path string) string { return path + ".fdattest.json" }

func digestFile(path string) (string, int64, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return "", 0, errors.New("attested configuration must be a regular file, not a symlink")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), int64(len(b)), nil
}

func Sign(key operatorapproval.PrivateKeyFile, path, kind string, ttl time.Duration, now time.Time) (Envelope, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return Envelope{}, errors.New("configuration kind is required")
	}
	if ttl <= 0 || ttl > 365*24*time.Hour {
		return Envelope{}, errors.New("attestation ttl must be greater than zero and at most 365d")
	}
	digest, size, err := digestFile(path)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	p := payload{Version: Version, Kind: kind, FileSHA256: digest, FileSize: size, Approver: key.Approver, KeyID: key.KeyID, SignedAt: now.UTC().Truncate(time.Second), ExpiresAt: now.UTC().Add(ttl).Truncate(time.Second), Nonce: base64.RawURLEncoding.EncodeToString(nonce)}
	message, _ := json.Marshal(p)
	signature, err := operatorapproval.SignDetached(key, message)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Version: p.Version, Kind: p.Kind, FileSHA256: p.FileSHA256, FileSize: p.FileSize, Approver: p.Approver, KeyID: p.KeyID, SignedAt: p.SignedAt, ExpiresAt: p.ExpiresAt, Nonce: p.Nonce, Signature: signature}, nil
}

func Verify(ring operatorapproval.Keyring, env Envelope, path, expectedKind string, now time.Time) error {
	if env.Version != Version {
		return errors.New("unsupported configuration attestation version")
	}
	if env.Kind != expectedKind {
		return fmt.Errorf("configuration attestation kind mismatch: expected %s found %s", expectedKind, env.Kind)
	}
	if env.Approver == "" || env.KeyID == "" || env.Nonce == "" || env.Signature == "" {
		return errors.New("configuration attestation identity fields are required")
	}
	if env.ExpiresAt.Before(env.SignedAt) || !now.UTC().Before(env.ExpiresAt) {
		return errors.New("configuration attestation expired")
	}
	if env.SignedAt.After(now.UTC().Add(2 * time.Minute)) {
		return errors.New("configuration attestation signed in the future")
	}
	digest, size, err := digestFile(path)
	if err != nil {
		return err
	}
	if digest != env.FileSHA256 || size != env.FileSize {
		return errors.New("configuration file digest or size does not match attestation")
	}
	p := payload{Version: env.Version, Kind: env.Kind, FileSHA256: env.FileSHA256, FileSize: env.FileSize, Approver: env.Approver, KeyID: env.KeyID, SignedAt: env.SignedAt, ExpiresAt: env.ExpiresAt, Nonce: env.Nonce}
	message, _ := json.Marshal(p)
	return operatorapproval.VerifyDetached(ring, env.KeyID, env.Approver, env.Signature, message)
}

func Load(path string) (Envelope, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Envelope{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Envelope{}, fmt.Errorf("attestation must not be group/world writable: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&env); err != nil {
		return env, err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return env, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return env, err
	}
	if env.Version == "" || env.Signature == "" {
		return env, errors.New("invalid configuration attestation")
	}
	return env, nil
}

func Write(path string, env Envelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func VerifySidecar(ring operatorapproval.Keyring, configPath, kind string, now time.Time) (Envelope, error) {
	env, err := Load(SidecarPath(configPath))
	if err != nil {
		return env, err
	}
	return env, Verify(ring, env, configPath, kind, now)
}
