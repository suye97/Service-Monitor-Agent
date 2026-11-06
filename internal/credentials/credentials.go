// Package credentials encrypts JSON credentials with a passphrase. The on-disk
// format is a versioned JSON envelope; callers supply the passphrase separately.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

const (
	version     = 1
	saltSize    = 16
	nonceSize   = 12
	keySize     = 32
	maxFileSize = 16 << 20
)

var ErrInvalidCredentials = errors.New("invalid passphrase or encrypted credentials")

type envelope struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	Cipher  string `json:"cipher"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

// Encrypt validates and encrypts a JSON document. The result is a portable JSON
// envelope containing the salt, nonce, and ciphertext, never the passphrase.
func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	if !json.Valid(plaintext) {
		return nil, errors.New("credentials plaintext must be valid JSON")
	}
	if passphrase == "" {
		return nil, errors.New("passphrase is required")
	}
	salt := make([]byte, saltSize)
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 4, keySize))
	if err != nil {
		return nil, err
	}
	data := cipher.NewGCM // deliberately instantiate below with checked error
	g, err := data(block)
	if err != nil {
		return nil, err
	}
	// The header parameters are fixed by version; binding the version as associated
	// data prevents an attacker from relabeling a ciphertext as another version.
	ciphertext := g.Seal(nil, nonce, plaintext, []byte("servicemonitoragent:credentials:v1"))
	e := envelope{Version: version, KDF: "argon2id-m3-t65536-p4", Cipher: "aes-256-gcm", Salt: base64.StdEncoding.EncodeToString(salt), Nonce: base64.StdEncoding.EncodeToString(nonce), Data: base64.StdEncoding.EncodeToString(ciphertext)}
	return json.MarshalIndent(e, "", "  ")
}

// Decrypt authenticates and returns the original JSON document. Incorrect
// passphrases and tampered ciphertext return ErrInvalidCredentials.
func Decrypt(blob []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, ErrInvalidCredentials
	}
	var e envelope
	if err := json.Unmarshal(blob, &e); err != nil {
		return nil, fmt.Errorf("parse encrypted credentials: %w", err)
	}
	if e.Version != version || e.KDF != "argon2id-m3-t65536-p4" || e.Cipher != "aes-256-gcm" {
		return nil, errors.New("unsupported encrypted credential format")
	}
	salt, err := base64.StdEncoding.DecodeString(e.Salt)
	if err != nil || len(salt) != saltSize {
		return nil, ErrInvalidCredentials
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil || len(nonce) != nonceSize {
		return nil, ErrInvalidCredentials
	}
	data, err := base64.StdEncoding.DecodeString(e.Data)
	if err != nil || len(data) < 16 {
		return nil, ErrInvalidCredentials
	}
	block, err := aes.NewCipher(argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 4, keySize))
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := g.Open(nil, nonce, data, []byte("servicemonitoragent:credentials:v1"))
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if !json.Valid(plaintext) {
		return nil, ErrInvalidCredentials
	}
	return plaintext, nil
}

// Save atomically replaces a credentials file with owner-only permissions.
func Save(path string, plaintext []byte, passphrase string) error {
	ciphertext, err := Encrypt(plaintext, passphrase)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(ciphertext); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Load reads an encrypted JSON document, bounded before key derivation.
func Load(path, passphrase string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileSize {
		return nil, errors.New("encrypted credentials file too large")
	}
	return Decrypt(b, passphrase)
}
