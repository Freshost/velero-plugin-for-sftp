package sftp

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Credentials represents the structured YAML credentials file.
//
// Example with SSH key:
//
//	user: uXXXXXX
//	privateKey: |
//	  -----BEGIN OPENSSH PRIVATE KEY-----
//	  b3BlbnNzaC1rZXktdjEAAAAA...
//	  -----END OPENSSH PRIVATE KEY-----
//	knownHosts: |
//	  [uXXXXXX.your-storagebox.de]:23 ssh-ed25519 AAAAC3NzaC1...
//
// Example with password:
//
//	user: uXXXXXX
//	password: mysecretpassword
//	knownHosts: |
//	  [uXXXXXX.your-storagebox.de]:23 ssh-ed25519 AAAAC3NzaC1...
type Credentials struct {
	User                 string `yaml:"user"`
	Password             string `yaml:"password,omitempty"`
	PrivateKey           string `yaml:"privateKey,omitempty"`
	PrivateKeyPassphrase string `yaml:"privateKeyPassphrase,omitempty"`
	KnownHosts           string `yaml:"knownHosts,omitempty"`
}

// parseCredentialsFile reads and parses a YAML credentials file.
func parseCredentialsFile(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading credentials file %s: %w", path, err)
	}

	creds := &Credentials{}
	if err := yaml.Unmarshal(data, creds); err != nil {
		return nil, fmt.Errorf("parsing credentials file %s: %w", path, err)
	}

	if creds.User == "" {
		return nil, fmt.Errorf("credentials file %s: 'user' is required", path)
	}
	if creds.PrivateKey == "" && creds.Password == "" {
		return nil, fmt.Errorf("credentials file %s: 'privateKey' or 'password' is required", path)
	}

	return creds, nil
}
