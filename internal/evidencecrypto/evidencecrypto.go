package evidencecrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	Version = "0.1"
	magic   = "FDE1"
)

type KeyFile struct {
	Version   string    `json:"version"`
	KeyID     string    `json:"key_id"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
}

type Cipher struct {
	KeyID string
	aead  cipher.AEAD
}

func Generate(now time.Time) (KeyFile, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return KeyFile{}, err
	}
	sum := sha256.Sum256(raw)
	return KeyFile{Version: Version, KeyID: "aes256gcm-" + hex.EncodeToString(sum[:8]), Key: base64.StdEncoding.EncodeToString(raw), CreatedAt: now.UTC()}, nil
}

func WriteKey(path string, key KeyFile) error {
	if key.Version == "" {
		key.Version = Version
	}
	b, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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

func Load(path string) (*Cipher, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("evidence key must be 0600 or stricter: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var k KeyFile
	if err := strictJSON(b, &k); err != nil {
		return nil, err
	}
	if k.Version != Version || strings.TrimSpace(k.KeyID) == "" {
		return nil, errors.New("invalid evidence key file")
	}
	raw, err := base64.StdEncoding.DecodeString(k.Key)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("invalid AES-256 key material")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i] = 0
	}
	return &Cipher{KeyID: k.KeyID, aead: aead}, nil
}

func (c *Cipher) Seal(plaintext, associatedData []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("evidence cipher is not initialized")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := c.aead.Seal(nil, nonce, plaintext, associatedData)
	keyID := []byte(c.KeyID)
	if len(keyID) > 65535 {
		return nil, errors.New("key id too long")
	}
	out := make([]byte, 0, 4+2+len(keyID)+1+len(nonce)+len(ciphertext))
	out = append(out, []byte(magic)...)
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], uint16(len(keyID)))
	out = append(out, n[:]...)
	out = append(out, keyID...)
	out = append(out, byte(len(nonce)))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func (c *Cipher) Open(encoded, associatedData []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("evidence cipher is not initialized")
	}
	if len(encoded) < 7 || string(encoded[:4]) != magic {
		return nil, errors.New("invalid encrypted evidence header")
	}
	keyLen := int(binary.BigEndian.Uint16(encoded[4:6]))
	pos := 6
	if len(encoded) < pos+keyLen+1 {
		return nil, errors.New("truncated encrypted evidence")
	}
	keyID := string(encoded[pos : pos+keyLen])
	pos += keyLen
	if keyID != c.KeyID {
		return nil, fmt.Errorf("evidence key mismatch: artifact=%s configured=%s", keyID, c.KeyID)
	}
	nonceLen := int(encoded[pos])
	pos++
	if nonceLen != c.aead.NonceSize() || len(encoded) < pos+nonceLen+c.aead.Overhead() {
		return nil, errors.New("invalid encrypted evidence nonce or payload")
	}
	nonce := encoded[pos : pos+nonceLen]
	pos += nonceLen
	return c.aead.Open(nil, nonce, encoded[pos:], associatedData)
}

func (c *Cipher) WriteFile(path string, plaintext, associatedData []byte) error {
	encoded, err := c.Seal(plaintext, associatedData)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
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

func (c *Cipher) ReadFile(path string, associatedData []byte) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return c.Open(b, associatedData)
}

func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	_, err = io.ReadFull(f, b[:])
	return err == nil && string(b[:]) == magic
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

// ActiveKeyID reports the key used for new evidence writes.
func (c *Cipher) ActiveKeyID() string {
	if c == nil {
		return ""
	}
	return c.KeyID
}

// FileCipher is the minimal encrypted-evidence contract used by the daemon.
type FileCipher interface {
	WriteFile(path string, plaintext, associatedData []byte) error
	ReadFile(path string, associatedData []byte) ([]byte, error)
	ActiveKeyID() string
}

// ArtifactKeyID returns the key identity embedded in an encrypted evidence file.
func ArtifactKeyID(encoded []byte) (string, error) {
	if len(encoded) < 7 || string(encoded[:4]) != magic {
		return "", errors.New("invalid encrypted evidence header")
	}
	keyLen := int(binary.BigEndian.Uint16(encoded[4:6]))
	pos := 6
	if keyLen < 1 || len(encoded) < pos+keyLen+1 {
		return "", errors.New("truncated encrypted evidence")
	}
	return string(encoded[pos : pos+keyLen]), nil
}
