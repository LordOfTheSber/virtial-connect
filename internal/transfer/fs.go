package transfer

import (
	"io"
	"os"

	"github.com/pkg/sftp"
)

type Walker interface {
	Step() bool
	Err() error
	Path() string
	Stat() os.FileInfo
}

type RemoteFS interface {
	Stat(string) (os.FileInfo, error)
	Walk(string) Walker
	Open(string) (io.ReadCloser, error)
	Create(string) (io.WriteCloser, error)
	MkdirAll(string) error
}

type sftpFS struct{ c *sftp.Client }

type sftpWalker struct{ w *sftp.Walker }

func NewSFTPFS(c *sftp.Client) RemoteFS                   { return &sftpFS{c: c} }
func (s *sftpFS) Stat(p string) (os.FileInfo, error)      { return s.c.Stat(p) }
func (s *sftpFS) Walk(p string) Walker                    { return &sftpWalker{w: s.c.Walk(p)} }
func (s *sftpFS) Open(p string) (io.ReadCloser, error)    { return s.c.Open(p) }
func (s *sftpFS) Create(p string) (io.WriteCloser, error) { return s.c.Create(p) }
func (s *sftpFS) MkdirAll(p string) error                 { return s.c.MkdirAll(p) }

func (w *sftpWalker) Step() bool        { return w.w.Step() }
func (w *sftpWalker) Err() error        { return w.w.Err() }
func (w *sftpWalker) Path() string      { return w.w.Path() }
func (w *sftpWalker) Stat() os.FileInfo { return w.w.Stat() }
