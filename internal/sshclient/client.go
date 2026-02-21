package sshclient

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"virtial-connect/internal/models"
	"virtial-connect/internal/util"
)

type Client struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
	mu         sync.Mutex
}

type ConnectConfig struct {
	Host          string
	Port          int
	Username      string
	Password      string
	PrivateKey    string
	Passphrase    string
	KnownHosts    string
	ConnectTimout time.Duration
	OnUnknownHost func(host, fingerprint string) (bool, error)
}

func (c *Client) Connect(cfg ConnectConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	hostKeyCallback, err := c.hostKeyCallback(cfg)
	if err != nil {
		return err
	}

	auth, err := buildAuth(cfg)
	if err != nil {
		return err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.ConnectTimout,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := net.DialTimeout("tcp", addr, cfg.ConnectTimout)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		return fmt.Errorf("ssh handshake: %w", err)
	}
	c.sshClient = ssh.NewClient(cc, chans, reqs)
	c.sftpClient, err = sftp.NewClient(c.sshClient, sftp.MaxPacket(1<<15))
	if err != nil {
		c.sshClient.Close()
		return fmt.Errorf("sftp init: %w", err)
	}
	return nil
}

func (c *Client) hostKeyCallback(cfg ConnectConfig) (ssh.HostKeyCallback, error) {
	if cfg.KnownHosts == "" {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	if err := os.MkdirAll(path.Dir(cfg.KnownHosts), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(cfg.KnownHosts); os.IsNotExist(err) {
		if writeErr := os.WriteFile(cfg.KnownHosts, []byte{}, 0o600); writeErr != nil {
			return nil, writeErr
		}
	}
	cb, err := knownhosts.New(cfg.KnownHosts)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		if _, ok := err.(*knownhosts.KeyError); !ok {
			return err
		}
		fp := md5Fingerprint(key)
		if cfg.OnUnknownHost == nil {
			return err
		}
		ok, decisionErr := cfg.OnUnknownHost(hostname, fp)
		if decisionErr != nil || !ok {
			if decisionErr != nil {
				return decisionErr
			}
			return fmt.Errorf("host key rejected")
		}
		line := knownhosts.Line([]string{hostname}, key)
		f, fileErr := os.OpenFile(cfg.KnownHosts, os.O_APPEND|os.O_WRONLY, 0o600)
		if fileErr != nil {
			return fileErr
		}
		defer f.Close()
		if _, writeErr := io.WriteString(f, line+"\n"); writeErr != nil {
			return writeErr
		}
		return nil
	}, nil
}

func md5Fingerprint(key ssh.PublicKey) string {
	sum := md5.Sum(key.Marshal())
	raw := hex.EncodeToString(sum[:])
	parts := make([]string, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		parts = append(parts, raw[i:i+2])
	}
	return strings.Join(parts, ":")
}

func buildAuth(cfg ConnectConfig) ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != "" {
		b, err := os.ReadFile(cfg.PrivateKey)
		if err != nil {
			return nil, err
		}
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(b)
		}
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth method configured")
	}
	return methods, nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sftpClient != nil {
		_ = c.sftpClient.Close()
		c.sftpClient = nil
	}
	if c.sshClient != nil {
		_ = c.sshClient.Close()
		c.sshClient = nil
	}
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sftpClient != nil
}

func (c *Client) ListRemote(remotePath string) ([]models.FileEntry, error) {
	c.mu.Lock()
	s := c.sftpClient
	c.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("not connected")
	}
	remotePath = util.NormalizeRemotePath(remotePath)
	items, err := s.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}
	res := make([]models.FileEntry, 0, len(items)+1)
	if remotePath != "/" {
		res = append(res, models.FileEntry{Name: "..", Path: util.NormalizeRemotePath(path.Dir(remotePath)), IsDir: true})
	}
	for _, it := range items {
		mode := it.Mode()
		res = append(res, models.FileEntry{
			Name:    it.Name(),
			Path:    util.JoinRemote(remotePath, it.Name()),
			IsDir:   it.IsDir(),
			Size:    it.Size(),
			Mode:    mode.String(),
			ModTime: it.ModTime(),
			IsLink:  mode&os.ModeSymlink != 0,
		})
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Name == ".." {
			return true
		}
		if res[j].Name == ".." {
			return false
		}
		if res[i].IsDir != res[j].IsDir {
			return res[i].IsDir
		}
		return strings.ToLower(res[i].Name) < strings.ToLower(res[j].Name)
	})
	return res, nil
}

func (c *Client) SFTP() *sftp.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sftpClient
}
