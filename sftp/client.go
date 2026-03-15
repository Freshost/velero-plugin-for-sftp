/*
Copyright 2025 Freshost.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sftp

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Config holds the SFTP connection configuration.
type Config struct {
	Host                         string
	Port                         string
	User                         string
	Password                     string
	PrivateKeyPath               string
	PrivateKeyData               []byte // PEM-encoded SSH private key (takes precedence over PrivateKeyPath)
	PrivateKeyPassphrase         string
	KnownHostsPath               string
	KnownHostsData               string // known_hosts content (takes precedence over KnownHostsPath)
	InsecureSkipHostVerification bool
	BasePath                     string
	Timeout                      time.Duration
}

// Client wraps an SFTP client with automatic reconnection.
type Client struct {
	mu         sync.Mutex
	config     Config
	log        logrus.FieldLogger
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

// NewClient creates a new reconnecting SFTP client.
func NewClient(cfg Config, log logrus.FieldLogger) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		config: cfg,
		log:    log,
	}
}

// Connect establishes the SSH and SFTP connections.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connect()
}

func (c *Client) connect() error {
	authMethods, err := c.buildAuthMethods()
	if err != nil {
		return fmt.Errorf("building auth methods: %w", err)
	}

	hostKeyCallback, err := c.buildHostKeyCallback()
	if err != nil {
		return fmt.Errorf("building host key callback: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            c.config.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.config.Timeout,
	}

	addr := fmt.Sprintf("%s:%s", c.config.Host, c.config.Port)
	c.log.Infof("Connecting to SFTP server %s as %s", addr, c.config.User)

	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("SSH dial %s: %w", addr, err)
	}

	sftpClient, err := sftp.NewClient(sshClient,
		sftp.MaxConcurrentRequestsPerFile(64),
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
	)
	if err != nil {
		sshClient.Close()
		return fmt.Errorf("creating SFTP client: %w", err)
	}

	c.sshClient = sshClient
	c.sftpClient = sftpClient
	c.log.Info("SFTP connection established")
	return nil
}

// SFTP returns a live SFTP filesystem, reconnecting if necessary.
func (c *Client) SFTP() (sftpFS, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sftpClient != nil {
		if _, err := c.sftpClient.Getwd(); err == nil {
			return &realSFTP{c: c.sftpClient}, nil
		}
		c.log.Warn("SFTP connection lost, reconnecting...")
		c.close()
	}

	if err := c.connect(); err != nil {
		return nil, err
	}
	return &realSFTP{c: c.sftpClient}, nil
}

// Close closes both SFTP and SSH connections.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.close()
}

func (c *Client) close() error {
	var firstErr error
	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			firstErr = err
		}
		c.sftpClient = nil
	}
	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		c.sshClient = nil
	}
	return firstErr
}

func (c *Client) buildAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// SSH key auth: prefer in-memory key data over file path
	keyBytes := c.config.PrivateKeyData
	if len(keyBytes) == 0 && c.config.PrivateKeyPath != "" {
		var err error
		keyBytes, err = os.ReadFile(c.config.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("reading private key %s: %w", c.config.PrivateKeyPath, err)
		}
	}

	if len(keyBytes) > 0 {
		var signer ssh.Signer
		var err error
		if c.config.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(c.config.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("parsing private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if c.config.Password != "" {
		methods = append(methods, ssh.Password(c.config.Password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication method configured: provide credentials via credentialsFile, privateKeyPath, or password")
	}
	return methods, nil
}

func (c *Client) buildHostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.config.InsecureSkipHostVerification {
		c.log.Warn("Host key verification is disabled - this is insecure")
		return ssh.InsecureIgnoreHostKey(), nil
	}

	// If known_hosts content is provided inline (from credentials file),
	// write it to a temp file since knownhosts.New() requires a file path.
	if c.config.KnownHostsData != "" {
		tmpFile, err := os.CreateTemp("", "known_hosts-*")
		if err != nil {
			return nil, fmt.Errorf("creating temp known_hosts file: %w", err)
		}
		if _, err := tmpFile.WriteString(c.config.KnownHostsData); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return nil, fmt.Errorf("writing temp known_hosts file: %w", err)
		}
		tmpFile.Close()

		callback, err := knownhosts.New(tmpFile.Name())
		if err != nil {
			os.Remove(tmpFile.Name())
			return nil, fmt.Errorf("parsing known_hosts: %w", err)
		}
		// Temp file can be removed after parsing — knownhosts loads it into memory.
		os.Remove(tmpFile.Name())
		return callback, nil
	}

	path := c.config.KnownHostsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory for known_hosts: %w", err)
		}
		path = home + "/.ssh/known_hosts"
	}

	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts from %s: %w", path, err)
	}
	return callback, nil
}
