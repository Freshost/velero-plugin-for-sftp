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
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// encryptor handles age encryption and decryption of objects.
type encryptor struct {
	identities []age.Identity
	recipient  age.Recipient
}

// newEncryptor creates an encryptor from an age identity file.
func newEncryptor(identityPath string) (*encryptor, error) {
	f, err := os.Open(identityPath)
	if err != nil {
		return nil, fmt.Errorf("opening identity file %s: %w", identityPath, err)
	}
	defer f.Close()
	return newEncryptorFromReader(f)
}

// newEncryptorFromString creates an encryptor from an age identity string
// (a single AGE-SECRET-KEY-... line).
func newEncryptorFromString(identityLine string) (*encryptor, error) {
	return newEncryptorFromReader(strings.NewReader(identityLine))
}

func newEncryptorFromReader(r io.Reader) (*encryptor, error) {
	identities, err := age.ParseIdentities(r)
	if err != nil {
		return nil, fmt.Errorf("parsing age identity: %w", err)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("no age identities found")
	}

	var recipient age.Recipient
	switch id := identities[0].(type) {
	case *age.X25519Identity:
		recipient = id.Recipient()
	case *age.HybridIdentity:
		recipient = id.Recipient()
	default:
		return nil, fmt.Errorf("unsupported age identity type %T (supported: X25519, post-quantum hybrid)", identities[0])
	}

	return &encryptor{
		identities: identities,
		recipient:  recipient,
	}, nil
}

// encrypt wraps a writer with age encryption. The returned WriteCloser
// MUST be closed to finalize the encryption.
func (e *encryptor) encrypt(dst io.Writer) (io.WriteCloser, error) {
	return age.Encrypt(dst, e.recipient)
}

// decrypt wraps a reader with age decryption.
func (e *encryptor) decrypt(src io.Reader) (io.Reader, error) {
	return age.Decrypt(src, e.identities...)
}

// decryptReadCloser combines a decrypted reader with the underlying
// file closer so that closing it releases the SFTP file handle.
type decryptReadCloser struct {
	io.Reader
	closer io.Closer
}

func (d *decryptReadCloser) Close() error {
	return d.closer.Close()
}
