package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// tagRe matches the "[fg:bg:attrs]" color tags this app emits, so logged
// lines can be stripped down to plain text.
var tagRe = regexp.MustCompile(`\[[^][]*:[^][]*:[^][]*\]`)

// stripFormatting removes tview color tags and reverses tview.Escape so log
// files contain plain, human-readable text.
func stripFormatting(s string) string {
	return tagRe.ReplaceAllString(tview.Unescape(s), "")
}

// sanitizeLogName neutralizes path separators and traversal sequences so a
// server-supplied channel/nick name can't be used to write outside log_dir.
func sanitizeLogName(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(name)
	if name == "" {
		name = "_"
	}
	return name
}

// logMessage appends text to the per-buffer log file when log_enabled is on
// and the buffer is a channel or query window.
func (a *App) logMessage(b *Buffer, text string) {
	if b == nil || (b.Kind != BufChannel && b.Kind != BufQuery) {
		return
	}
	if !a.settings.Bool("log_enabled") {
		return
	}
	a.connMu.Lock()
	server := a.serverName
	a.connMu.Unlock()
	if server == "" {
		server = "unknown"
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	f, err := a.openLogFileLocked(server, b.Name)
	if err != nil {
		return
	}
	tsFormat := a.settings.Get("log_timestamp")
	if tsFormat == "" {
		tsFormat = "15:04:05"
	}
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(tsFormat), stripFormatting(text))
}

// openLogFileLocked returns the cached log file handle for server/name,
// opening and caching it on first use. Caller must hold a.logMu.
func (a *App) openLogFileLocked(server, name string) (*os.File, error) {
	if a.logFiles == nil {
		a.logFiles = map[string]*os.File{}
	}
	key := strings.ToLower(server) + "/" + strings.ToLower(name)
	if f, ok := a.logFiles[key]; ok {
		return f, nil
	}
	dir := a.settings.Get("log_dir")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "irclogs")
	}
	dir = filepath.Join(dir, sanitizeLogName(server))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, sanitizeLogName(name)+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	a.logFiles[key] = f
	return f, nil
}

// closeLogs flushes and closes every open per-buffer log file handle.
func (a *App) closeLogs() {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	for _, f := range a.logFiles {
		f.Close()
	}
	a.logFiles = nil
}
