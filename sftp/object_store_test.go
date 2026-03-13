package sftp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// --- fake filesystem backed by a temp directory ---

type fakeFS struct {
	root string
}

func newFakeFS(t *testing.T) *fakeFS {
	t.Helper()
	return &fakeFS{root: t.TempDir()}
}

func (f *fakeFS) localPath(p string) string {
	return filepath.Join(f.root, filepath.FromSlash(p))
}

func (f *fakeFS) MkdirAll(path string) error {
	return os.MkdirAll(f.localPath(path), 0755)
}

func (f *fakeFS) OpenFile(path string, flags int) (sftpFile, error) {
	return os.OpenFile(f.localPath(path), flags, 0644)
}

func (f *fakeFS) Open(path string) (sftpFile, error) {
	return os.Open(f.localPath(path))
}

func (f *fakeFS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(f.localPath(path))
}

func (f *fakeFS) ReadDir(path string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(f.localPath(path))
	if err != nil {
		return nil, err
	}
	var infos []os.FileInfo
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (f *fakeFS) Remove(path string) error {
	return os.Remove(f.localPath(path))
}

func (f *fakeFS) Rename(old, new string) error {
	return os.Rename(f.localPath(old), f.localPath(new))
}

func (f *fakeFS) PosixRename(old, new string) error {
	return os.Rename(f.localPath(old), f.localPath(new))
}

func (f *fakeFS) Walk(root string) sftpWalker {
	var entries []walkEntry
	localRoot := f.localPath(root)
	filepath.Walk(localRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(f.root, path)
		entries = append(entries, walkEntry{
			path: "/" + filepath.ToSlash(rel),
			info: info,
		})
		return nil
	})
	return &fakeWalker{entries: entries, index: -1}
}

type walkEntry struct {
	path string
	info os.FileInfo
}

type fakeWalker struct {
	entries []walkEntry
	index   int
}

func (w *fakeWalker) Step() bool {
	w.index++
	return w.index < len(w.entries)
}

func (w *fakeWalker) Path() string    { return w.entries[w.index].path }
func (w *fakeWalker) Stat() os.FileInfo { return w.entries[w.index].info }
func (w *fakeWalker) Err() error      { return nil }

// --- fake provider ---

type fakeProvider struct {
	fs sftpFS
}

func (f *fakeProvider) SFTP() (sftpFS, error) { return f.fs, nil }
func (f *fakeProvider) Connect() error         { return nil }

// --- helpers ---

func testLogger() logrus.FieldLogger {
	l := logrus.New()
	l.Out = io.Discard
	return logrus.NewEntry(l)
}

func newTestStore(t *testing.T) (*ObjectStore, *fakeFS) {
	t.Helper()
	fs := newFakeFS(t)
	return &ObjectStore{
		log:      testLogger(),
		client:   &fakeProvider{fs: fs},
		basePath: "/home",
	}, fs
}

// --- tests ---

func TestValidateConfigKeys(t *testing.T) {
	t.Run("valid keys", func(t *testing.T) {
		err := validateConfigKeys(map[string]string{
			"host": "example.com", "port": "22", "bucket": "b",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		err := validateConfigKeys(map[string]string{
			"host": "example.com", "bogus": "val",
		})
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(err.Error(), "bogus") {
			t.Fatalf("error should mention 'bogus', got: %v", err)
		}
	})
}

func TestObjectPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		bucket   string
		key      string
		want     string
	}{
		{"with basePath", "/home", "backups", "data.tar", "/home/backups/data.tar"},
		{"without basePath", "", "backups", "data.tar", "/backups/data.tar"},
		{"nested key", "/home", "backups", "ns/pod/file.json", "/home/backups/ns/pod/file.json"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &ObjectStore{basePath: tc.basePath}
			got := o.objectPath(tc.bucket, tc.key)
			if got != tc.want {
				t.Fatalf("objectPath(%q, %q) = %q, want %q", tc.bucket, tc.key, got, tc.want)
			}
		})
	}
}

func TestPutAndGetObject(t *testing.T) {
	store, _ := newTestStore(t)
	content := "hello velero backup"

	if err := store.PutObject("bucket", "test/file.txt", strings.NewReader(content)); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	rc, err := store.GetObject("bucket", "test/file.txt")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != content {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestPutAndGetObjectWithEncryption(t *testing.T) {
	store, fs := newTestStore(t)
	content := "secret data for encryption test"

	enc, err := newEncryptorFromString("AGE-SECRET-KEY-1AFC8AH6ZQT4CQATW5Q848J72XETMTNN47JVN8EH05PA4RG7PRFRSC0Y3RT")
	if err != nil {
		t.Fatalf("newEncryptorFromString: %v", err)
	}
	store.encryptor = enc

	if err := store.PutObject("bucket", "secret.dat", strings.NewReader(content)); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// Verify file on disk is encrypted (starts with age header)
	raw, err := os.ReadFile(filepath.Join(fs.root, "home", "bucket", "secret.dat"))
	if err != nil {
		t.Fatalf("reading raw file: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("age-encryption.org/v1")) {
		t.Fatal("file on disk should be age-encrypted")
	}

	// GetObject should return decrypted content
	rc, err := store.GetObject("bucket", "secret.dat")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != content {
		t.Fatalf("decrypted = %q, want %q", got, content)
	}
}

func TestObjectExists(t *testing.T) {
	store, _ := newTestStore(t)

	exists, err := store.ObjectExists("bucket", "nope.txt")
	if err != nil {
		t.Fatalf("ObjectExists: %v", err)
	}
	if exists {
		t.Fatal("should not exist")
	}

	if err := store.PutObject("bucket", "yes.txt", strings.NewReader("hi")); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	exists, err = store.ObjectExists("bucket", "yes.txt")
	if err != nil {
		t.Fatalf("ObjectExists: %v", err)
	}
	if !exists {
		t.Fatal("should exist")
	}
}

func TestDeleteObject(t *testing.T) {
	store, _ := newTestStore(t)

	if err := store.PutObject("bucket", "del.txt", strings.NewReader("bye")); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	if err := store.DeleteObject("bucket", "del.txt"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}

	exists, _ := store.ObjectExists("bucket", "del.txt")
	if exists {
		t.Fatal("should not exist after delete")
	}

	// Deleting non-existent should not error
	if err := store.DeleteObject("bucket", "nonexistent.txt"); err != nil {
		t.Fatalf("DeleteObject non-existent: %v", err)
	}
}

func TestListCommonPrefixes(t *testing.T) {
	store, _ := newTestStore(t)

	store.PutObject("bucket", "backups/full/data.tar", strings.NewReader("a"))
	store.PutObject("bucket", "backups/incremental/data.tar", strings.NewReader("b"))
	store.PutObject("bucket", "backups/file.txt", strings.NewReader("c"))

	prefixes, err := store.ListCommonPrefixes("bucket", "backups/", "/")
	if err != nil {
		t.Fatalf("ListCommonPrefixes: %v", err)
	}

	if len(prefixes) != 2 {
		t.Fatalf("got %d prefixes, want 2: %v", len(prefixes), prefixes)
	}
	if prefixes[0] != "backups/full/" || prefixes[1] != "backups/incremental/" {
		t.Fatalf("prefixes = %v, want [backups/full/ backups/incremental/]", prefixes)
	}
}

func TestListObjects(t *testing.T) {
	store, _ := newTestStore(t)

	store.PutObject("bucket", "prefix/a.txt", strings.NewReader("a"))
	store.PutObject("bucket", "prefix/b.txt", strings.NewReader("b"))
	store.PutObject("bucket", "prefix/sub/c.txt", strings.NewReader("c"))
	store.PutObject("bucket", "other/d.txt", strings.NewReader("d"))

	keys, err := store.ListObjects("bucket", "prefix/")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3: %v", len(keys), keys)
	}

	// Check all returned keys start with prefix/
	for _, k := range keys {
		if !strings.HasPrefix(k, "prefix/") {
			t.Fatalf("key %q should start with prefix/", k)
		}
	}
}

func TestListCommonPrefixesNonExistent(t *testing.T) {
	store, _ := newTestStore(t)

	prefixes, err := store.ListCommonPrefixes("bucket", "nope/", "/")
	if err != nil {
		t.Fatalf("ListCommonPrefixes: %v", err)
	}
	if prefixes != nil {
		t.Fatalf("expected nil, got %v", prefixes)
	}
}

func TestCreateSignedURL(t *testing.T) {
	store, _ := newTestStore(t)
	url, err := store.CreateSignedURL("bucket", "key", 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateSignedURL should not return error, got: %v", err)
	}
	if url != "" {
		t.Fatalf("CreateSignedURL should return empty string, got: %q", url)
	}
}

func TestParseCredentialsFile(t *testing.T) {
	t.Run("password auth", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "creds.yaml")
		os.WriteFile(f, []byte("user: admin\npassword: secret\n"), 0644)

		creds, err := parseCredentialsFile(f)
		if err != nil {
			t.Fatalf("parseCredentialsFile: %v", err)
		}
		if creds.User != "admin" || creds.Password != "secret" {
			t.Fatalf("unexpected creds: %+v", creds)
		}
	})

	t.Run("missing user", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "creds.yaml")
		os.WriteFile(f, []byte("password: secret\n"), 0644)

		_, err := parseCredentialsFile(f)
		if err == nil {
			t.Fatal("expected error for missing user")
		}
	})

	t.Run("no auth method", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "creds.yaml")
		os.WriteFile(f, []byte("user: admin\n"), 0644)

		_, err := parseCredentialsFile(f)
		if err == nil {
			t.Fatal("expected error when no auth method provided")
		}
	})
}
