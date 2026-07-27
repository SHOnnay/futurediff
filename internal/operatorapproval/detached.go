package operatorapproval

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// SignDetached signs arbitrary canonical bytes with an operator private key.
func SignDetached(key PrivateKeyFile, message []byte) (string, error) {
	priv, err := decodePrivate(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, message)), nil
}

// VerifyDetached verifies arbitrary canonical bytes against one enabled key.
func VerifyDetached(ring Keyring, keyID, approver, signature string, message []byte) error {
	var record *PublicKey
	for i := range ring.Keys {
		if ring.Keys[i].KeyID == keyID {
			record = &ring.Keys[i]
			break
		}
	}
	if record == nil || !record.Enabled {
		return errors.New("operator key is not trusted")
	}
	if record.Approver != approver {
		return errors.New("operator approver mismatch")
	}
	pub, err := base64.StdEncoding.DecodeString(record.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid trusted public key")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid detached signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), message, sig) {
		return errors.New("detached signature verification failed")
	}
	return nil
}
