package editor

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

type OpenedFile struct {
	RemotePath    string
	TempPath      string
	OriginalMTime time.Time
	ReadOnly      bool
}

type Service struct {
	sftp *sftp.Client
}

func New(s *sftp.Client) *Service {
	return &Service{sftp: s}
}

func (s *Service) Open(remotePath string, maxEditBytes int64) (*OpenedFile, []byte, error) {
	st, err := s.sftp.Stat(remotePath)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.sftp.Open(remotePath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	tmp, err := os.CreateTemp("", "virtial-edit-*")
	if err != nil {
		return nil, nil, err
	}
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		return nil, nil, err
	}
	opened := &OpenedFile{RemotePath: remotePath, TempPath: tmp.Name(), OriginalMTime: st.ModTime(), ReadOnly: st.Size() > maxEditBytes}
	return opened, data, nil
}

func (s *Service) SaveAtomic(f *OpenedFile, content []byte, force bool) error {
	st, err := s.sftp.Stat(f.RemotePath)
	if err != nil {
		return err
	}
	if !force && !st.ModTime().Equal(f.OriginalMTime) {
		return fmt.Errorf("remote file changed since open")
	}
	tmpRemote := path.Join(path.Dir(f.RemotePath), ".vc_tmp_"+path.Base(f.RemotePath)+"_"+time.Now().Format("150405"))
	wf, err := s.sftp.Create(tmpRemote)
	if err != nil {
		return err
	}
	if _, err := wf.Write(content); err != nil {
		wf.Close()
		return err
	}
	if err := wf.Close(); err != nil {
		return err
	}
	if err := s.sftp.Rename(tmpRemote, f.RemotePath); err != nil {
		_ = s.sftp.Remove(tmpRemote)
		return err
	}
	f.OriginalMTime = time.Now()
	return nil
}

func BasicSyntaxHint(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".json", ".yaml", ".yml", ".sh":
		return ext[1:]
	default:
		return "plain"
	}
}
