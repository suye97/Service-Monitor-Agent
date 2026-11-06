// Package sshclient executes fixed remote commands using strict known_hosts verification.
package sshclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Client is reusable and holds no open network connection between commands.
type Client struct {
	addr   string
	config *ssh.ClientConfig
}

// New requires an existing OpenSSH known_hosts file. Unknown or changed host
// keys are rejected. addr must contain an explicit port (for example host:22).
func New(addr, user, password, knownHostsPath string) (*Client, error) {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("ssh address: %w", err)
	}
	if strings.TrimSpace(user) == "" || password == "" {
		return nil, errors.New("ssh user and password are required")
	}
	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	return &Client{addr: addr, config: &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: callback, Timeout: 10 * time.Second}}, nil
}

// Run starts one SSH command. Cancellation closes its connection and session.
// stderr is included only in returned errors to keep metric stdout parseable.
func (c *Client) Run(ctx context.Context, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, c.addr, c.config)
	if err != nil {
		return "", fmt.Errorf("SSH handshake/host key: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("remote command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
