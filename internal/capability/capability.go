package capability

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

const Version = "1"
const MaxTTL = 15 * time.Minute

type Token struct {
	Version      string    `json:"version"`
	CapabilityID string    `json:"capability_id"`
	SubjectUID   uint32    `json:"subject_uid"`
	OperationID  string    `json:"operation_id"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Approver     string    `json:"approver"`
	KeyID        string    `json:"key_id"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Nonce        string    `json:"nonce"`
	Signature    string    `json:"signature"`
}

type payload struct {
	Version      string    `json:"version"`
	CapabilityID string    `json:"capability_id"`
	SubjectUID   uint32    `json:"subject_uid"`
	OperationID  string    `json:"operation_id"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Approver     string    `json:"approver"`
	KeyID        string    `json:"key_id"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Nonce        string    `json:"nonce"`
}

func Sign(key operatorapproval.PrivateKeyFile, uid uint32, operationID, resourceID string, ttl time.Duration, now time.Time) (Token, error) {
	operationID = strings.TrimSpace(operationID)
	resourceID = strings.TrimSpace(resourceID)
	if operationID == "" {
		return Token{}, errors.New("operation id is required")
	}
	if ttl <= 0 || ttl > MaxTTL {
		return Token{}, fmt.Errorf("capability ttl must be greater than zero and at most %s", MaxTTL)
	}
	privRaw, err := base64.StdEncoding.DecodeString(key.PrivateKey)
	if err != nil || len(privRaw) != ed25519.PrivateKeySize {
		return Token{}, errors.New("invalid private key")
	}
	pub := ed25519.PrivateKey(privRaw).Public().(ed25519.PublicKey)
	if base64.StdEncoding.EncodeToString(pub) != key.PublicKey {
		return Token{}, errors.New("private/public key mismatch")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Token{}, err
	}
	idRaw := make([]byte, 16)
	if _, err := rand.Read(idRaw); err != nil {
		return Token{}, err
	}
	p := payload{Version: Version, CapabilityID: "cap-" + hex.EncodeToString(idRaw), SubjectUID: uid, OperationID: operationID, ResourceID: resourceID, Approver: key.Approver, KeyID: key.KeyID, IssuedAt: now.UTC().Truncate(time.Second), ExpiresAt: now.UTC().Add(ttl).Truncate(time.Second), Nonce: base64.RawURLEncoding.EncodeToString(nonce)}
	msg, _ := json.Marshal(p)
	sig := ed25519.Sign(ed25519.PrivateKey(privRaw), msg)
	return Token{Version: p.Version, CapabilityID: p.CapabilityID, SubjectUID: p.SubjectUID, OperationID: p.OperationID, ResourceID: p.ResourceID, Approver: p.Approver, KeyID: p.KeyID, IssuedAt: p.IssuedAt, ExpiresAt: p.ExpiresAt, Nonce: p.Nonce, Signature: base64.StdEncoding.EncodeToString(sig)}, nil
}

func Verify(ring operatorapproval.Keyring, token Token, uid uint32, operationID, resourceID string, now time.Time) error {
	if token.Version != Version {
		return errors.New("unsupported capability version")
	}
	if token.CapabilityID == "" || token.Nonce == "" || token.KeyID == "" || token.Approver == "" {
		return errors.New("capability identity fields are required")
	}
	if token.SubjectUID != uid || token.OperationID != operationID || token.ResourceID != resourceID {
		return errors.New("capability scope mismatch")
	}
	if token.ExpiresAt.Before(token.IssuedAt) || !now.UTC().Before(token.ExpiresAt) {
		return errors.New("capability expired")
	}
	if token.ExpiresAt.Sub(token.IssuedAt) > MaxTTL {
		return errors.New("capability lifetime exceeds maximum")
	}
	if token.IssuedAt.After(now.UTC().Add(2 * time.Minute)) {
		return errors.New("capability issued in the future")
	}
	var trusted *operatorapproval.PublicKey
	for i := range ring.Keys {
		if ring.Keys[i].KeyID == token.KeyID {
			trusted = &ring.Keys[i]
			break
		}
	}
	if trusted == nil || !trusted.Enabled {
		return errors.New("capability key is not trusted")
	}
	if trusted.Approver != token.Approver {
		return errors.New("capability approver mismatch")
	}
	pub, err := base64.StdEncoding.DecodeString(trusted.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid trusted public key")
	}
	sig, err := base64.StdEncoding.DecodeString(token.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid capability signature")
	}
	p := payload{Version: token.Version, CapabilityID: token.CapabilityID, SubjectUID: token.SubjectUID, OperationID: token.OperationID, ResourceID: token.ResourceID, Approver: token.Approver, KeyID: token.KeyID, IssuedAt: token.IssuedAt, ExpiresAt: token.ExpiresAt, Nonce: token.Nonce}
	msg, _ := json.Marshal(p)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		return errors.New("capability signature verification failed")
	}
	return nil
}

func EncodeCompact(token Token) (string, error) {
	b, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func DecodeCompact(encoded string) (Token, error) {
	if len(encoded) > 16<<10 {
		return Token{}, errors.New("capability header exceeds 16 KiB")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return Token{}, errors.New("invalid capability encoding")
	}
	var token Token
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&token); err != nil {
		return Token{}, err
	}
	return token, nil
}
func Digest(token Token) string {
	b, _ := json.Marshal(token)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
