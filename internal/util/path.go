package util

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func NormalizeRemotePath(p string) string {
	if p == "" {
		return "/"
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func JoinRemote(base, name string) string {
	return NormalizeRemotePath(path.Join(base, name))
}

func ResolveConflictName(dest string) string {
	ext := filepath.Ext(dest)
	base := strings.TrimSuffix(dest, ext)
	stamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_copy_%s%s", base, stamp, ext)
}
