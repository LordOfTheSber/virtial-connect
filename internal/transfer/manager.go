package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"virtial-connect/internal/models"
	"virtial-connect/internal/util"
)

type ProgressFn func(task models.TransferTask)

type Manager struct {
	fs         RemoteFS
	workers    int
	queue      chan *models.TransferTask
	onProgress ProgressFn
	mu         sync.Mutex
	tasks      map[string]*models.TransferTask
}

func New(fs RemoteFS, workers int, progress ProgressFn) *Manager {
	if workers < 1 {
		workers = 2
	}
	m := &Manager{
		fs:         fs,
		workers:    workers,
		queue:      make(chan *models.TransferTask, 128),
		onProgress: progress,
		tasks:      map[string]*models.TransferTask{},
	}
	for i := 0; i < m.workers; i++ {
		go m.workerLoop()
	}
	return m
}

func (m *Manager) Enqueue(task *models.TransferTask) {
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	task.Status = models.TransferQueued
	task.CancelToken = make(chan struct{})
	m.notify(*task)
	m.queue <- task
}

func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tasks[id]; t != nil && t.CancelToken != nil {
		close(t.CancelToken)
		t.CancelToken = nil
	}
}

func (m *Manager) workerLoop() {
	for task := range m.queue {
		start := time.Now()
		task.StartedAt = start
		task.Status = models.TransferRunning
		m.notify(*task)
		ctx, cancel := context.WithCancel(context.Background())
		go func(ch chan struct{}) {
			if ch == nil {
				return
			}
			<-ch
			cancel()
		}(task.CancelToken)
		err := m.runTask(ctx, task)
		cancel()
		task.FinishedAt = time.Now()
		switch {
		case err == nil:
			task.Status = models.TransferCompleted
		case err == context.Canceled:
			task.Status = models.TransferCanceled
			task.Error = "canceled"
		default:
			task.Status = models.TransferFailed
			task.Error = err.Error()
		}
		m.notify(*task)
	}
}

func (m *Manager) runTask(ctx context.Context, task *models.TransferTask) error {
	if task.Direction == models.Download {
		return m.download(ctx, task)
	}
	return m.upload(ctx, task)
}

func (m *Manager) download(ctx context.Context, task *models.TransferTask) error {
	stat, err := m.fs.Stat(task.SourcePath)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return m.downloadDir(ctx, task.SourcePath, task.DestPath, task)
	}
	return m.copyRemoteToLocal(ctx, task.SourcePath, task.DestPath, task)
}

func (m *Manager) upload(ctx context.Context, task *models.TransferTask) error {
	stat, err := os.Stat(task.SourcePath)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return m.uploadDir(ctx, task.SourcePath, task.DestPath, task)
	}
	return m.copyLocalToRemote(ctx, task.SourcePath, task.DestPath, task)
}

func (m *Manager) downloadDir(ctx context.Context, remoteDir, localDir string, task *models.TransferTask) error {
	walker := m.fs.Walk(remoteDir)
	for walker.Step() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walker.Err() != nil {
			return walker.Err()
		}
		rel := stringsTrimPrefix(walker.Path(), remoteDir)
		target := filepath.Join(localDir, filepath.FromSlash(rel))
		if walker.Stat().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := m.copyRemoteToLocal(ctx, walker.Path(), target, task); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) uploadDir(ctx context.Context, localDir, remoteDir string, task *models.TransferTask) error {
	return filepath.WalkDir(localDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, relErr := filepath.Rel(localDir, p)
		if relErr != nil {
			return relErr
		}
		target := util.JoinRemote(remoteDir, filepath.ToSlash(rel))
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return m.fs.MkdirAll(target)
		}
		return m.copyLocalToRemote(ctx, p, target, task)
	})
}

func (m *Manager) copyRemoteToLocal(ctx context.Context, remotePath, localPath string, task *models.TransferTask) error {
	rf, err := m.fs.Open(remotePath)
	if err != nil {
		return err
	}
	defer rf.Close()
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	lf, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	return m.copyWithProgress(ctx, lf, rf, task)
}

func (m *Manager) copyLocalToRemote(ctx context.Context, localPath, remotePath string, task *models.TransferTask) error {
	lf, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := m.fs.MkdirAll(path.Dir(remotePath)); err != nil {
		return err
	}
	rf, err := m.fs.Create(remotePath)
	if err != nil {
		return err
	}
	defer rf.Close()
	return m.copyWithProgress(ctx, rf, lf, task)
}

func (m *Manager) copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, task *models.TransferTask) error {
	buf := make([]byte, 32*1024)
	last := time.Now()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := src.Read(buf)
		if n > 0 {
			wn, werr := dst.Write(buf[:n])
			if werr != nil {
				return werr
			}
			if wn != n {
				return fmt.Errorf("short write")
			}
			task.DoneBytes += int64(n)
			dur := time.Since(last)
			if dur >= 500*time.Millisecond {
				elapsed := time.Since(task.StartedAt).Seconds()
				if elapsed > 0 {
					task.SpeedBps = float64(task.DoneBytes) / elapsed
				}
				if task.TotalBytes > 0 && task.SpeedBps > 0 {
					remaining := float64(task.TotalBytes-task.DoneBytes) / task.SpeedBps
					task.ETA = time.Duration(remaining) * time.Second
				}
				m.notify(*task)
				last = time.Now()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (m *Manager) notify(task models.TransferTask) {
	if m.onProgress != nil {
		m.onProgress(task)
	}
}

func stringsTrimPrefix(full, prefix string) string {
	res := strings.TrimPrefix(full, prefix)
	res = strings.TrimPrefix(res, "/")
	return res
}
