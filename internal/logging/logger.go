package logging

import (
	"fmt"
	"sync"
	"time"
)

type Logger struct {
	mu      sync.Mutex
	entries []string
	hook    func(string)
}

func New(hook func(string)) *Logger {
	return &Logger{hook: hook, entries: make([]string, 0, 256)}
}

func (l *Logger) Info(format string, args ...any) {
	l.append("INFO", fmt.Sprintf(format, args...))
}

func (l *Logger) Error(format string, args ...any) {
	l.append("ERROR", fmt.Sprintf(format, args...))
}

func (l *Logger) append(level, message string) {
	line := fmt.Sprintf("%s [%s] %s", time.Now().Format(time.RFC3339), level, message)
	l.mu.Lock()
	l.entries = append(l.entries, line)
	hook := l.hook
	l.mu.Unlock()
	if hook != nil {
		hook(line)
	}
}

func (l *Logger) Entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.entries))
	copy(out, l.entries)
	return out
}
