package tests

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"virtial-connect/internal/models"
	"virtial-connect/internal/transfer"
)

type fakeInfo struct {
	name string
	size int64
	dir  bool
}

func (f fakeInfo) Name() string { return f.name }
func (f fakeInfo) Size() int64  { return f.size }
func (f fakeInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (f fakeInfo) ModTime() time.Time { return time.Now() }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

type fakeWalker struct {
	paths []string
	infos []os.FileInfo
	i     int
}

func (w *fakeWalker) Step() bool        { w.i++; return w.i <= len(w.paths) }
func (w *fakeWalker) Err() error        { return nil }
func (w *fakeWalker) Path() string      { return w.paths[w.i-1] }
func (w *fakeWalker) Stat() os.FileInfo { return w.infos[w.i-1] }

type fakeFS struct{ files map[string][]byte }

func (f *fakeFS) Stat(p string) (os.FileInfo, error) {
	b := f.files[p]
	return fakeInfo{name: filepath.Base(p), size: int64(len(b))}, nil
}
func (f *fakeFS) Walk(p string) transfer.Walker {
	return &fakeWalker{paths: []string{p}, infos: []os.FileInfo{fakeInfo{name: filepath.Base(p), dir: true}}}
}
func (f *fakeFS) Open(p string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.files[p])), nil
}
func (f *fakeFS) Create(string) (io.WriteCloser, error) { return nopWriteCloser{io.Discard}, nil }
func (f *fakeFS) MkdirAll(string) error                 { return nil }

type nopWriteCloser struct{ io.Writer }

func (n nopWriteCloser) Close() error { return nil }

func TestTransferDownloadSmoke(t *testing.T) {
	dir := t.TempDir()
	fs := &fakeFS{files: map[string][]byte{"/remote/a.txt": []byte("hello")}}
	mgr := transfer.New(fs, 1, nil)
	task := &models.TransferTask{ID: "1", Direction: models.Download, SourcePath: "/remote/a.txt", DestPath: filepath.Join(dir, "a.txt"), TotalBytes: 5}
	mgr.Enqueue(task)
	time.Sleep(300 * time.Millisecond)
	data, err := os.ReadFile(task.DestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected %s", data)
	}
}
