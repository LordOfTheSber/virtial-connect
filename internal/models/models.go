package models

import "time"

type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

type ConnectionProfile struct {
	Name            string     `json:"name"`
	Host            string     `json:"host"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	AuthMethod      AuthMethod `json:"auth_method"`
	PasswordEnc     string     `json:"password_enc,omitempty"`
	PrivateKeyPath  string     `json:"private_key_path,omitempty"`
	PassphraseEnc   string     `json:"passphrase_enc,omitempty"`
	LastRemotePath  string     `json:"last_remote_path,omitempty"`
	LastLocalPath   string     `json:"last_local_path,omitempty"`
	LastConnectedAt time.Time  `json:"last_connected_at,omitempty"`
}

type FileEntry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	Mode    string
	ModTime time.Time
	IsLink  bool
}

type TransferDirection string

const (
	Download TransferDirection = "download"
	Upload   TransferDirection = "upload"
)

type ConflictPolicy string

const (
	ConflictOverwrite ConflictPolicy = "overwrite"
	ConflictSkip      ConflictPolicy = "skip"
	ConflictRename    ConflictPolicy = "rename"
	ConflictCompare   ConflictPolicy = "compare"
)

type TransferStatus string

const (
	TransferQueued    TransferStatus = "queued"
	TransferRunning   TransferStatus = "running"
	TransferCompleted TransferStatus = "completed"
	TransferFailed    TransferStatus = "failed"
	TransferCanceled  TransferStatus = "canceled"
)

type TransferTask struct {
	ID          string
	Direction   TransferDirection
	SourcePath  string
	DestPath    string
	TotalBytes  int64
	DoneBytes   int64
	SpeedBps    float64
	ETA         time.Duration
	Status      TransferStatus
	Error       string
	StartedAt   time.Time
	FinishedAt  time.Time
	CancelToken chan struct{}
}
