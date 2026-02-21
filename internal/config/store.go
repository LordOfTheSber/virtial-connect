package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"virtial-connect/internal/models"
)

type Settings struct {
	Profiles []models.ConnectionProfile `json:"profiles"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	appDir := filepath.Join(dir, "virtial-connect")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(appDir, "settings.json")}, nil
}

func (s *Store) Load() (*Settings, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil
		}
		return nil, err
	}
	var st Settings
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *Store) Save(st *Settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}
