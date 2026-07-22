package ui

import (
	"sort"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// BufferKind distinguishes status / channel / query / server buffers.
type BufferKind int

const (
	BufStatus BufferKind = iota
	BufServer
	BufChannel
	BufQuery
	BufDCC
)

// ActivityLevel tracks per-buffer unread weight for the status bar.
type ActivityLevel int

const (
	ActNone   ActivityLevel = iota
	ActLow                  // join/part/server chatter
	ActMsg                  // a real message landed
	ActHilite               // own nick was mentioned, or query/notice
)

// Buffer is a single named window backed by a tview.TextView.
type Buffer struct {
	Name     string
	Kind     BufferKind
	View     *tview.TextView
	Topic    string
	Modes    string
	Nicks    map[string]string // nick -> prefix ("@","+","")
	Activity ActivityLevel

	mu sync.Mutex
}

// MaxBufferLines is the per-window scrollback cap; older lines are trimmed
// so memory and redraw time stay bounded for long-running sessions.
const MaxBufferLines = 5000

// NewBuffer creates an empty buffer with a configured TextView.
func NewBuffer(name string, kind BufferKind) *Buffer {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true).
		SetMaxLines(MaxBufferLines)
	tv.SetBorder(false)
	tv.SetBackgroundColor(tcell.ColorDefault)
	// Track the end so new lines auto-scroll into view. Manual scrolling
	// (PgUp/Home) disables tracking; PgDn/End re-enables it.
	tv.ScrollToEnd()
	return &Buffer{
		Name:  name,
		Kind:  kind,
		View:  tv,
		Nicks: map[string]string{},
	}
}

// AddNick records a nick in a channel buffer.
func (b *Buffer) AddNick(nick, prefix string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Nicks[nick] = prefix
}

// RemoveNick drops a nick from this buffer.
func (b *Buffer) RemoveNick(nick string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.Nicks, nick)
}

// HasNick reports whether the buffer currently contains the given nick.
func (b *Buffer) HasNick(nick string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.Nicks[nick]
	return ok
}

// RenameNick updates a nick entry.
func (b *Buffer) RenameNick(old, new string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.Nicks[old]
	if !ok {
		return false
	}
	delete(b.Nicks, old)
	b.Nicks[new] = p
	return true
}

// NickList returns the sorted nick list (prefix+nick).
func (b *Buffer) NickList() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.Nicks))
	for n, p := range b.Nicks {
		out = append(out, p+n)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}
