package util

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRemotePath(t *testing.T) {
	got := NormalizeRemotePath(`var\\log/../tmp`)
	if got != "/var/tmp" {
		t.Fatalf("got %s", got)
	}
}

func TestJoinRemote(t *testing.T) {
	if got := JoinRemote("/home/user", "docs"); got != "/home/user/docs" {
		t.Fatalf("join got %s", got)
	}
}

func TestResolveConflictName(t *testing.T) {
	name := ResolveConflictName(filepath.Join("C:\\tmp", "a.txt"))
	if !strings.Contains(name, "_copy_") || !strings.HasSuffix(name, ".txt") {
		t.Fatalf("unexpected %s", name)
	}
}
