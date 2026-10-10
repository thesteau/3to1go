package integrations

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errVault = errors.New("cannot read integration secrets; check the encryption key and persisted integration files")

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errVault
	}
	key, err := m.readKey(false)
	if err != nil {
		return err
	}
	box, err := newBox(key)
	if err != nil || len(data) < 1+box.NonceSize()+box.Overhead() || data[0] != 1 {
		return errVault
	}
	plain, err := box.Open(nil, data[1:1+box.NonceSize()], data[1+box.NonceSize():], []byte("3to1go-integrations-v1:"+m.app))
	if err != nil || json.Unmarshal(plain, &m.destinations) != nil {
		return errVault
	}
	if len(m.destinations) > 10 {
		return errVault
	}
	m.key = key
	return nil
}

func newBox(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (m *Manager) readKey(create bool) ([]byte, error) {
	data, err := os.ReadFile(m.keyPath)
	if errors.Is(err, os.ErrNotExist) && create && !m.externalKey {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, errVault
		}
		if err := os.MkdirAll(filepath.Dir(m.keyPath), 0o700); err != nil {
			return nil, errVault
		}
		file, err := os.OpenFile(m.keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, errVault
		}
		_, writeErr := file.WriteString(hex.EncodeToString(key) + "\n")
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return nil, errVault
		}
		return key, nil
	}
	if err != nil {
		return nil, errVault
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		return nil, errVault
	}
	return key, nil
}

// persist writes an authenticated encrypted file atomically; the settings database has no secrets.
func (m *Manager) persist(destinations []storedDestination) error {
	key, err := m.readKey(m.key == nil)
	if err != nil {
		return err
	}
	if m.key != nil && !bytes.Equal(key, m.key) {
		return errVault
	}
	box, err := newBox(key)
	if err != nil {
		return errVault
	}
	plain, err := json.Marshal(destinations)
	if err != nil {
		return errVault
	}
	nonce := make([]byte, box.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return errVault
	}
	data := append([]byte{1}, nonce...)
	data = box.Seal(data, nonce, plain, []byte("3to1go-integrations-v1:"+m.app))
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return errVault
	}
	file, err := os.CreateTemp(filepath.Dir(m.path), ".integrations-*")
	if err != nil {
		return errVault
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return errVault
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errVault
	}
	if err := os.Rename(file.Name(), m.path); err != nil {
		return errVault
	}
	m.key = key
	return nil
}
