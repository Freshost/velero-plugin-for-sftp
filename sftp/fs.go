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
	"io"
	"os"

	pkgsftp "github.com/pkg/sftp"
)

// sftpFS abstracts the SFTP filesystem operations for testability.
type sftpFS interface {
	MkdirAll(path string) error
	OpenFile(path string, f int) (sftpFile, error)
	Open(path string) (sftpFile, error)
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.FileInfo, error)
	Remove(path string) error
	Rename(oldpath, newpath string) error
	PosixRename(oldpath, newpath string) error
	Walk(root string) sftpWalker
}

// sftpFile abstracts an SFTP file handle.
type sftpFile interface {
	io.Reader
	io.Writer
	io.Closer
	ReadFrom(r io.Reader) (int64, error)
}

// sftpWalker abstracts the SFTP directory walker.
type sftpWalker interface {
	Step() bool
	Path() string
	Stat() os.FileInfo
	Err() error
}

// sftpProvider abstracts the SFTP client connection for testability.
type sftpProvider interface {
	SFTP() (sftpFS, error)
	Connect() error
}

// realSFTP wraps *sftp.Client to implement sftpFS.
type realSFTP struct {
	c *pkgsftp.Client
}

func (r *realSFTP) MkdirAll(path string) error                 { return r.c.MkdirAll(path) }
func (r *realSFTP) Stat(path string) (os.FileInfo, error)      { return r.c.Stat(path) }
func (r *realSFTP) ReadDir(path string) ([]os.FileInfo, error) { return r.c.ReadDir(path) }
func (r *realSFTP) Remove(path string) error                   { return r.c.Remove(path) }
func (r *realSFTP) Rename(old, new string) error               { return r.c.Rename(old, new) }
func (r *realSFTP) PosixRename(old, new string) error          { return r.c.PosixRename(old, new) }
func (r *realSFTP) Walk(root string) sftpWalker                { return r.c.Walk(root) }

func (r *realSFTP) OpenFile(path string, f int) (sftpFile, error) {
	return r.c.OpenFile(path, f)
}

func (r *realSFTP) Open(path string) (sftpFile, error) {
	return r.c.Open(path)
}
