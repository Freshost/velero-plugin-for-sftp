package sftp

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	configHost                         = "host"
	configPort                         = "port"
	configUser                         = "user"
	configPassword                     = "password"
	configPrivateKeyPath               = "privateKeyPath"
	configPrivateKeyPassphrase         = "privateKeyPassphrase"
	configKnownHostsPath               = "knownHostsPath"
	configInsecureSkipHostVerification = "insecureSkipHostKeyVerification"
	configBasePath                     = "basePath"
	configTimeout                      = "timeout"
	configEncryptionKeyPath            = "encryptionKeyPath"
	configCredentialsFile              = "credentialsFile"
	configBucket                       = "bucket"
	configPrefix                       = "prefix"
)

var allowedConfigKeys = []string{
	configHost,
	configPort,
	configUser,
	configPassword,
	configPrivateKeyPath,
	configPrivateKeyPassphrase,
	configKnownHostsPath,
	configInsecureSkipHostVerification,
	configBasePath,
	configTimeout,
	configEncryptionKeyPath,
	configCredentialsFile,
	configBucket,
	configPrefix,
	// Kopia SFTP backend keys — passed through BSL config, not used by ObjectStore
	// but must be allowed so BSL validation doesn't reject them.
	"sftpHost",
	"sftpPort",
	"sftpPath",
	"sftpUsername",
	"sftpPassword",
	"sftpKeyPath",
	"sftpKeyData",
	"sftpKnownHostsData",
}

// ObjectStore implements the Velero ObjectStore interface for SFTP.
type ObjectStore struct {
	log       logrus.FieldLogger
	client    sftpProvider
	basePath  string
	encryptor *encryptor
}

// NewObjectStore creates a new uninitialized ObjectStore.
func NewObjectStore(log logrus.FieldLogger) *ObjectStore {
	return &ObjectStore{
		log: log,
	}
}

// Init initializes the ObjectStore with the given configuration.
func (o *ObjectStore) Init(config map[string]string) error {
	if err := validateConfigKeys(config); err != nil {
		return err
	}

	host := config[configHost]
	if host == "" {
		return fmt.Errorf("config key %q is required", configHost)
	}

	port := config[configPort]
	if port == "" {
		port = "22"
	}

	timeout := 30 * time.Second
	if t := config[configTimeout]; t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", t, err)
		}
		timeout = d
	}

	cfg := Config{
		Host:                         host,
		Port:                         port,
		InsecureSkipHostVerification: config[configInsecureSkipHostVerification] == "true",
		BasePath:                     strings.TrimRight(config[configBasePath], "/"),
		Timeout:                      timeout,
	}

	// Primary: structured YAML credentials file (from BSL credential field).
	if credsFile := config[configCredentialsFile]; credsFile != "" {
		creds, err := parseCredentialsFile(credsFile)
		if err != nil {
			return fmt.Errorf("parsing credentials file: %w", err)
		}
		cfg.User = creds.User
		if creds.PrivateKey != "" {
			cfg.PrivateKeyData = []byte(creds.PrivateKey)
		}
		if creds.PrivateKeyPassphrase != "" {
			cfg.PrivateKeyPassphrase = creds.PrivateKeyPassphrase
		}
		if creds.Password != "" {
			cfg.Password = creds.Password
		}
		if creds.KnownHosts != "" {
			cfg.KnownHostsData = creds.KnownHosts
		}
	} else {
		// Fallback: individual config keys (for manual setups without BSL credential).
		cfg.User = config[configUser]
		cfg.Password = config[configPassword]
		cfg.PrivateKeyPath = config[configPrivateKeyPath]
		cfg.PrivateKeyPassphrase = config[configPrivateKeyPassphrase]
		cfg.KnownHostsPath = config[configKnownHostsPath]
	}

	if cfg.User == "" {
		return fmt.Errorf("'user' is required (in credentials file or config)")
	}

	// Age encryption (separate from credentials — different security lifecycle).
	if keyPath := config[configEncryptionKeyPath]; keyPath != "" {
		enc, err := newEncryptor(keyPath)
		if err != nil {
			return fmt.Errorf("initializing encryption: %w", err)
		}
		o.encryptor = enc
		o.log.Info("Age encryption enabled")
	}

	o.basePath = cfg.BasePath
	o.client = NewClient(cfg, o.log)

	return o.client.Connect()
}

// PutObject uploads data from body to the object store.
func (o *ObjectStore) PutObject(bucket, key string, body io.Reader) error {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return fmt.Errorf("getting SFTP client: %w", err)
	}

	fullPath := o.objectPath(bucket, key)
	dir := path.Dir(fullPath)

	if err := sftpClient.MkdirAll(dir); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Write to temp file first for atomicity
	tmpPath := fullPath + ".tmp"
	f, err := sftpClient.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("creating temp file %s: %w", tmpPath, err)
	}

	if o.encryptor != nil {
		w, err := o.encryptor.encrypt(f)
		if err != nil {
			f.Close()
			sftpClient.Remove(tmpPath)
			return fmt.Errorf("creating encryptor for %s: %w", tmpPath, err)
		}
		if _, err := io.Copy(w, body); err != nil {
			w.Close()
			f.Close()
			sftpClient.Remove(tmpPath)
			return fmt.Errorf("encrypting to %s: %w", tmpPath, err)
		}
		if err := w.Close(); err != nil {
			f.Close()
			sftpClient.Remove(tmpPath)
			return fmt.Errorf("finalizing encryption for %s: %w", tmpPath, err)
		}
	} else {
		if _, err := f.ReadFrom(body); err != nil {
			f.Close()
			sftpClient.Remove(tmpPath)
			return fmt.Errorf("writing to %s: %w", tmpPath, err)
		}
	}

	if err := f.Close(); err != nil {
		sftpClient.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}

	// Atomic rename
	if err := sftpClient.PosixRename(tmpPath, fullPath); err != nil {
		// Fallback for servers without posix-rename extension
		sftpClient.Remove(fullPath)
		if err := sftpClient.Rename(tmpPath, fullPath); err != nil {
			return fmt.Errorf("renaming %s to %s: %w", tmpPath, fullPath, err)
		}
	}

	o.log.WithFields(logrus.Fields{"bucket": bucket, "key": key}).Debug("Object uploaded")
	return nil
}

// ObjectExists checks whether an object exists.
func (o *ObjectStore) ObjectExists(bucket, key string) (bool, error) {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return false, fmt.Errorf("getting SFTP client: %w", err)
	}

	fullPath := o.objectPath(bucket, key)
	_, err = sftpClient.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", fullPath, err)
	}
	return true, nil
}

// GetObject retrieves an object from the store.
func (o *ObjectStore) GetObject(bucket, key string) (io.ReadCloser, error) {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return nil, fmt.Errorf("getting SFTP client: %w", err)
	}

	fullPath := o.objectPath(bucket, key)
	f, err := sftpClient.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", fullPath, err)
	}

	if o.encryptor != nil {
		r, err := o.encryptor.decrypt(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("decrypting %s: %w", fullPath, err)
		}
		return &decryptReadCloser{Reader: r, closer: f}, nil
	}

	return f, nil
}

// ListCommonPrefixes returns common prefixes (subdirectories) under the given prefix.
func (o *ObjectStore) ListCommonPrefixes(bucket, prefix, delimiter string) ([]string, error) {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return nil, fmt.Errorf("getting SFTP client: %w", err)
	}

	searchDir := o.objectPath(bucket, prefix)

	entries, err := sftpClient.ReadDir(searchDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading directory %s: %w", searchDir, err)
	}

	var prefixes []string
	for _, entry := range entries {
		if entry.IsDir() {
			prefixes = append(prefixes, prefix+entry.Name()+delimiter)
		}
	}

	sort.Strings(prefixes)
	return prefixes, nil
}

// ListObjects returns all object keys matching the given prefix.
func (o *ObjectStore) ListObjects(bucket, prefix string) ([]string, error) {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return nil, fmt.Errorf("getting SFTP client: %w", err)
	}

	bucketPath := o.bucketPath(bucket)
	searchPath := o.objectPath(bucket, prefix)

	// Determine the directory to walk and the prefix filter
	searchDir := searchPath
	prefixFilter := ""
	info, err := sftpClient.Stat(searchPath)
	if err != nil || !info.IsDir() {
		// searchPath is not a directory — walk the parent and filter by prefix
		searchDir = path.Dir(searchPath)
		prefixFilter = path.Base(searchPath)
	}

	var keys []string
	walker := sftpClient.Walk(searchDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			o.log.WithError(err).Warn("Error walking SFTP directory")
			continue
		}
		if walker.Stat().IsDir() {
			continue
		}

		filePath := walker.Path()
		relPath := strings.TrimPrefix(filePath, bucketPath+"/")

		if prefixFilter != "" {
			// Only include files whose name starts with the prefix filter
			fileName := path.Base(filePath)
			if !strings.HasPrefix(fileName, prefixFilter) {
				continue
			}
		}

		keys = append(keys, relPath)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	return keys, nil
}

// DeleteObject removes an object from the store.
func (o *ObjectStore) DeleteObject(bucket, key string) error {
	sftpClient, err := o.client.SFTP()
	if err != nil {
		return fmt.Errorf("getting SFTP client: %w", err)
	}

	fullPath := o.objectPath(bucket, key)
	if err := sftpClient.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("removing %s: %w", fullPath, err)
	}

	o.log.WithFields(logrus.Fields{"bucket": bucket, "key": key}).Debug("Object deleted")
	return nil
}

// CreateSignedURL is not supported for SFTP storage.
// Returns ("", nil) so the Velero download-request controller marks the
// request as processed instead of requeuing it indefinitely.
func (o *ObjectStore) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	return "", nil
}

func (o *ObjectStore) objectPath(bucket, key string) string {
	if o.basePath != "" {
		return path.Join(o.basePath, bucket, key)
	}
	return path.Join("/", bucket, key)
}

func (o *ObjectStore) bucketPath(bucket string) string {
	if o.basePath != "" {
		return path.Join(o.basePath, bucket)
	}
	return path.Join("/", bucket)
}

func validateConfigKeys(config map[string]string) error {
	allowed := make(map[string]bool, len(allowedConfigKeys))
	for _, k := range allowedConfigKeys {
		allowed[k] = true
	}
	for k := range config {
		if !allowed[k] {
			return fmt.Errorf("unknown config key %q", k)
		}
	}
	return nil
}
