package evidencecrypto

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const KeyringVersion = "0.1"

type KeyReference struct {
	KeyID   string `json:"key_id"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type KeyringFile struct {
	Version     string         `json:"version"`
	ActiveKeyID string         `json:"active_key_id"`
	Keys        []KeyReference `json:"keys"`
}

type Keyring struct {
	active *Cipher
	keys   map[string]*Cipher
}

func (k *Keyring) ActiveKeyID() string {
	if k == nil || k.active == nil {
		return ""
	}
	return k.active.KeyID
}
func (k *Keyring) WriteFile(path string, plaintext, associatedData []byte) error {
	if k == nil || k.active == nil {
		return errors.New("evidence keyring has no active key")
	}
	return k.active.WriteFile(path, plaintext, associatedData)
}
func (k *Keyring) ReadFile(path string, associatedData []byte) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	id, err := ArtifactKeyID(b)
	if err != nil {
		return nil, err
	}
	c := k.keys[id]
	if c == nil {
		return nil, fmt.Errorf("evidence key %q is not available", id)
	}
	return c.Open(b, associatedData)
}

func LoadKeyring(path string) (*Keyring, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("evidence keyring must not be group/world writable: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f KeyringFile
	if err := strictJSON(b, &f); err != nil {
		return nil, err
	}
	if err := validateKeyringFile(f); err != nil {
		return nil, err
	}
	base := filepath.Dir(path)
	keys := map[string]*Cipher{}
	for _, ref := range f.Keys {
		if !ref.Enabled {
			continue
		}
		p := ref.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		c, err := Load(p)
		if err != nil {
			return nil, fmt.Errorf("load evidence key %s: %w", ref.KeyID, err)
		}
		if c.KeyID != ref.KeyID {
			return nil, fmt.Errorf("evidence key id mismatch for %s", ref.Path)
		}
		keys[c.KeyID] = c
	}
	active := keys[f.ActiveKeyID]
	if active == nil {
		return nil, errors.New("active evidence key is disabled or unavailable")
	}
	return &Keyring{active: active, keys: keys}, nil
}

func WriteKeyring(path string, f KeyringFile) error {
	if err := validateKeyringFile(f); err != nil {
		return err
	}
	sort.Slice(f.Keys, func(i, j int) bool { return f.Keys[i].KeyID < f.Keys[j].KeyID })
	b, err := json.MarshalIndent(f, "", "  ")
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

func InitializeKeyring(keyringPath, keyPath string, now time.Time) (KeyringFile, KeyFile, error) {
	key, err := Generate(now)
	if err != nil {
		return KeyringFile{}, KeyFile{}, err
	}
	if err := WriteKey(keyPath, key); err != nil {
		return KeyringFile{}, KeyFile{}, err
	}
	refPath := relativeReference(keyringPath, keyPath)
	f := KeyringFile{Version: KeyringVersion, ActiveKeyID: key.KeyID, Keys: []KeyReference{{KeyID: key.KeyID, Path: refPath, Enabled: true}}}
	if err := WriteKeyring(keyringPath, f); err != nil {
		return KeyringFile{}, KeyFile{}, err
	}
	return f, key, nil
}

func RotateKeyring(keyringPath, newKeyPath string, disableOld bool, now time.Time) (KeyringFile, KeyFile, error) {
	b, err := os.ReadFile(keyringPath)
	if err != nil {
		return KeyringFile{}, KeyFile{}, err
	}
	var f KeyringFile
	if err := strictJSON(b, &f); err != nil {
		return f, KeyFile{}, err
	}
	if err := validateKeyringFile(f); err != nil {
		return f, KeyFile{}, err
	}
	key, err := Generate(now)
	if err != nil {
		return f, KeyFile{}, err
	}
	if err := WriteKey(newKeyPath, key); err != nil {
		return f, KeyFile{}, err
	}
	if disableOld {
		for i := range f.Keys {
			f.Keys[i].Enabled = false
		}
	}
	f.Keys = append(f.Keys, KeyReference{KeyID: key.KeyID, Path: relativeReference(keyringPath, newKeyPath), Enabled: true})
	f.ActiveKeyID = key.KeyID
	if err := WriteKeyring(keyringPath, f); err != nil {
		return f, KeyFile{}, err
	}
	return f, key, nil
}

func validateKeyringFile(f KeyringFile) error {
	if f.Version != KeyringVersion || strings.TrimSpace(f.ActiveKeyID) == "" || len(f.Keys) == 0 {
		return errors.New("invalid evidence keyring")
	}
	seen := map[string]bool{}
	active := false
	for _, r := range f.Keys {
		if strings.TrimSpace(r.KeyID) == "" || strings.TrimSpace(r.Path) == "" || seen[r.KeyID] {
			return errors.New("invalid or duplicate evidence key reference")
		}
		seen[r.KeyID] = true
		if r.KeyID == f.ActiveKeyID && r.Enabled {
			active = true
		}
	}
	if !active {
		return errors.New("active evidence key must be enabled")
	}
	return nil
}
func relativeReference(keyringPath, keyPath string) string {
	abs, _ := filepath.Abs(keyPath)
	base, _ := filepath.Abs(filepath.Dir(keyringPath))
	rel, err := filepath.Rel(base, abs)
	if err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return rel
	}
	return abs
}
