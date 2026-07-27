package operatorapproval

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const Version = "0.1"

type PrivateKeyFile struct {
	Version    string    `json:"version"`
	KeyID      string    `json:"key_id"`
	Approver   string    `json:"approver"`
	PublicKey  string    `json:"public_key"`
	PrivateKey string    `json:"private_key"`
	CreatedAt  time.Time `json:"created_at"`
}

type PublicKey struct {
	KeyID     string `json:"key_id"`
	Approver  string `json:"approver"`
	PublicKey string `json:"public_key"`
	Enabled   bool   `json:"enabled"`
}

type Keyring struct {
	Version string      `json:"version"`
	Keys    []PublicKey `json:"keys"`
}

type Envelope struct {
	Version           string    `json:"version"`
	TransactionID     string    `json:"transaction_id"`
	TransactionDigest string    `json:"transaction_digest"`
	Approver          string    `json:"approver"`
	KeyID             string    `json:"key_id"`
	SignedAt          time.Time `json:"signed_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	Nonce             string    `json:"nonce"`
	Signature         string    `json:"signature"`
}

type signedPayload struct {
	Version           string    `json:"version"`
	TransactionID     string    `json:"transaction_id"`
	TransactionDigest string    `json:"transaction_digest"`
	Approver          string    `json:"approver"`
	KeyID             string    `json:"key_id"`
	SignedAt          time.Time `json:"signed_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	Nonce             string    `json:"nonce"`
}

func Generate(approver string, now time.Time) (PrivateKeyFile, PublicKey, error) {
	approver = strings.TrimSpace(approver)
	if approver == "" {
		return PrivateKeyFile{}, PublicKey{}, errors.New("approver is required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKeyFile{}, PublicKey{}, err
	}
	sum := sha256.Sum256(pub)
	id := "ed25519-" + hex.EncodeToString(sum[:8])
	rec := PrivateKeyFile{Version: Version, KeyID: id, Approver: approver, PublicKey: base64.StdEncoding.EncodeToString(pub), PrivateKey: base64.StdEncoding.EncodeToString(priv), CreatedAt: now.UTC()}
	return rec, PublicKey{KeyID: id, Approver: approver, PublicKey: rec.PublicKey, Enabled: true}, nil
}

func WritePrivate(path string, key PrivateKeyFile) error {
	b, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), 0o600)
}

func WriteKeyring(path string, ring Keyring) error {
	if ring.Version == "" {
		ring.Version = Version
	}
	sort.Slice(ring.Keys, func(i, j int) bool { return ring.Keys[i].KeyID < ring.Keys[j].KeyID })
	b, err := json.MarshalIndent(ring, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), 0o600)
}

func LoadPrivate(path string) (PrivateKeyFile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return PrivateKeyFile{}, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return PrivateKeyFile{}, fmt.Errorf("private key file must not be group/world accessible: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return PrivateKeyFile{}, err
	}
	var k PrivateKeyFile
	if err := strictJSON(b, &k); err != nil {
		return k, err
	}
	if k.Version != Version || k.KeyID == "" || k.Approver == "" {
		return k, errors.New("invalid private key file")
	}
	if _, err := decodePrivate(k); err != nil {
		return k, err
	}
	return k, nil
}

func LoadKeyring(path string) (Keyring, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Keyring{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Keyring{}, fmt.Errorf("keyring must not be group/world writable: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Keyring{}, err
	}
	var ring Keyring
	if err := strictJSON(b, &ring); err != nil {
		return ring, err
	}
	if ring.Version != Version || len(ring.Keys) == 0 {
		return ring, errors.New("invalid or empty approval keyring")
	}
	seen := map[string]bool{}
	for _, k := range ring.Keys {
		if k.KeyID == "" || k.Approver == "" || seen[k.KeyID] {
			return ring, errors.New("invalid or duplicate keyring entry")
		}
		seen[k.KeyID] = true
		raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return ring, fmt.Errorf("invalid public key %s", k.KeyID)
		}
	}
	return ring, nil
}

func Sign(key PrivateKeyFile, transactionID, digest string, ttl time.Duration, now time.Time) (Envelope, error) {
	transactionID, digest = strings.TrimSpace(transactionID), strings.TrimSpace(digest)
	if transactionID == "" || digest == "" {
		return Envelope{}, errors.New("transaction id and digest are required")
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		return Envelope{}, errors.New("approval ttl must be greater than zero and at most 24h")
	}
	priv, err := decodePrivate(key)
	if err != nil {
		return Envelope{}, err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return Envelope{}, err
	}
	p := signedPayload{Version: Version, TransactionID: transactionID, TransactionDigest: digest, Approver: key.Approver, KeyID: key.KeyID, SignedAt: now.UTC().Truncate(time.Second), ExpiresAt: now.UTC().Add(ttl).Truncate(time.Second), Nonce: base64.RawURLEncoding.EncodeToString(nonceBytes)}
	msg, _ := json.Marshal(p)
	sig := ed25519.Sign(priv, msg)
	return Envelope{Version: p.Version, TransactionID: p.TransactionID, TransactionDigest: p.TransactionDigest, Approver: p.Approver, KeyID: p.KeyID, SignedAt: p.SignedAt, ExpiresAt: p.ExpiresAt, Nonce: p.Nonce, Signature: base64.StdEncoding.EncodeToString(sig)}, nil
}

func Verify(ring Keyring, env Envelope, expectedTransaction, expectedDigest string, now time.Time) error {
	if env.Version != Version {
		return errors.New("unsupported approval envelope version")
	}
	if env.TransactionID != expectedTransaction || env.TransactionDigest != expectedDigest {
		return errors.New("approval envelope transaction or digest mismatch")
	}
	if env.Approver == "" || env.KeyID == "" || env.Nonce == "" {
		return errors.New("approval envelope identity fields are required")
	}
	if env.ExpiresAt.Before(env.SignedAt) || !now.UTC().Before(env.ExpiresAt) {
		return errors.New("approval envelope expired")
	}
	if env.SignedAt.After(now.UTC().Add(2 * time.Minute)) {
		return errors.New("approval envelope signed in the future")
	}
	var record *PublicKey
	for i := range ring.Keys {
		if ring.Keys[i].KeyID == env.KeyID {
			record = &ring.Keys[i]
			break
		}
	}
	if record == nil || !record.Enabled {
		return errors.New("approval key is not trusted")
	}
	if record.Approver != env.Approver {
		return errors.New("approval key approver mismatch")
	}
	pub, err := base64.StdEncoding.DecodeString(record.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid trusted public key")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid approval signature")
	}
	p := signedPayload{Version: env.Version, TransactionID: env.TransactionID, TransactionDigest: env.TransactionDigest, Approver: env.Approver, KeyID: env.KeyID, SignedAt: env.SignedAt, ExpiresAt: env.ExpiresAt, Nonce: env.Nonce}
	msg, _ := json.Marshal(p)
	if !ed25519.Verify(pub, msg, sig) {
		return errors.New("approval signature verification failed")
	}
	return nil
}

func SignatureReference(env Envelope) string {
	sum := sha256.Sum256([]byte(env.Signature))
	return fmt.Sprintf("ed25519:%s:%s", env.KeyID, hex.EncodeToString(sum[:]))
}

func decodePrivate(k PrivateKeyFile) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(k.PrivateKey)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key material")
	}
	pub := ed25519.PrivateKey(raw).Public().(ed25519.PublicKey)
	if base64.StdEncoding.EncodeToString(pub) != k.PublicKey {
		return nil, errors.New("private/public key mismatch")
	}
	return ed25519.PrivateKey(raw), nil
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Rotate generates a new key for an approver and updates a trusted keyring.
// Existing keys remain enabled by default to support overlap during rollout.
func Rotate(ring Keyring, approver string, disableOld bool, now time.Time) (Keyring, PrivateKeyFile, PublicKey, error) {
	if ring.Version == "" {
		ring.Version = Version
	}
	if ring.Version != Version {
		return Keyring{}, PrivateKeyFile{}, PublicKey{}, errors.New("unsupported keyring version")
	}
	priv, pub, err := Generate(approver, now)
	if err != nil {
		return Keyring{}, PrivateKeyFile{}, PublicKey{}, err
	}
	for _, existing := range ring.Keys {
		if existing.KeyID == pub.KeyID {
			return Keyring{}, PrivateKeyFile{}, PublicKey{}, errors.New("generated duplicate key id")
		}
	}
	if disableOld {
		for i := range ring.Keys {
			if ring.Keys[i].Approver == pub.Approver {
				ring.Keys[i].Enabled = false
			}
		}
	}
	ring.Keys = append(ring.Keys, pub)
	return ring, priv, pub, nil
}

// SetEnabled changes trust for one key while refusing to leave an approver with
// no enabled key unless allowNoEnabled is explicitly set.
func SetEnabled(ring Keyring, keyID string, enabled, allowNoEnabled bool) (Keyring, error) {
	found := false
	approver := ""
	for i := range ring.Keys {
		if ring.Keys[i].KeyID == keyID {
			found = true
			approver = ring.Keys[i].Approver
			ring.Keys[i].Enabled = enabled
			break
		}
	}
	if !found {
		return Keyring{}, fmt.Errorf("approval key %q not found", keyID)
	}
	if !enabled && !allowNoEnabled {
		remaining := 0
		for _, key := range ring.Keys {
			if key.Approver == approver && key.Enabled {
				remaining++
			}
		}
		if remaining == 0 {
			return Keyring{}, fmt.Errorf("refusing to disable the final enabled key for approver %q", approver)
		}
	}
	return ring, nil
}

// LoadEnvelope loads one strict signed approval envelope.
func LoadEnvelope(path string) (Envelope, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := strictJSON(b, &env); err != nil {
		return env, err
	}
	if env.Version != Version || env.TransactionID == "" || env.TransactionDigest == "" || env.Signature == "" {
		return env, errors.New("invalid approval envelope")
	}
	return env, nil
}
