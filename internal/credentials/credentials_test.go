package credentials

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEncryptDecryptAndSave(t *testing.T) {
	input := []byte(`{"servers":[{"host":"example.test","password":"sample-placeholder"}]}`)
	encrypted, err := Encrypt(input, "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("sample-placeholder")) {
		t.Fatal("plaintext appears in encrypted file")
	}
	output, err := Decrypt(encrypted, "test-passphrase")
	if err != nil || !bytes.Equal(input, output) {
		t.Fatalf("decrypt: %v", err)
	}
	if _, err := Decrypt(encrypted, "incorrect"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	encrypted[len(encrypted)-4] = '!'
	if _, err := Decrypt(encrypted, "test-passphrase"); err == nil {
		t.Fatal("tampering accepted")
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := Save(path, input, "test-passphrase"); err != nil {
		t.Fatal(err)
	}
	data, err := Load(path, "test-passphrase")
	if err != nil || !bytes.Equal(input, data) {
		t.Fatalf("load: %v", err)
	}
	if st, err := os.Stat(path); err != nil {
		t.Fatalf("file stat: %v", err)
	} else if runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
		t.Fatalf("file permissions: %v", st.Mode().Perm())
	}
	// Windows os.FileMode reports generic writable bits; ACL access is not represented by Perm().
}
func TestRejectPlaintextAndUnknownVersion(t *testing.T) {
	if _, err := Encrypt([]byte("not JSON"), "pass"); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	if _, err := Decrypt([]byte(`{"version":2}`), "pass"); err == nil {
		t.Fatal("accepted unknown version")
	}
}
