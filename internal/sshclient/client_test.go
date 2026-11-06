package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestRequiresKnownHostsAndValidAddress(t *testing.T) {
	if _, err := New("invalid", "user", "password", "missing"); err == nil {
		t.Fatal("accepted address without port")
	}
	if _, err := New("example.com:22", "user", "password", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("accepted missing known_hosts")
	}
}
func writeKnownHosts(path string) error {
	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pub, err := ssh.NewPublicKey(pubkey)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(knownhosts.Line([]string{"127.0.0.1"}, pub)+"\n"), 0600)
}
func TestCancelledRunDoesNotDial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := writeKnownHosts(path); err != nil {
		t.Fatal(err)
	}
	c, err := New("127.0.0.1:22", "user", "password", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Run(ctx, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
