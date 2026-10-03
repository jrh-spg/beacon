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

	mu        sync.Mutex
	modeFlags map[byte]string // channel mode letter -> parameter (if any)
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

// NickCount returns the number of nicks currently tracked for this buffer.
func (b *Buffer) NickCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.Nicks)
}

// Channel mode letter classes, used to decide whether a mode letter takes a
// parameter and whether it belongs in the buffer's displayed mode string.
// Without ISUPPORT CHANMODES tracking we fall back to the common defaults
// shared by most IRCd implementations.
var (
	chanModeListTypes  = map[byte]bool{'b': true, 'e': true, 'I': true}                       // ban/except/invex lists — not shown
	chanModeUserPrefix = map[byte]bool{'o': true, 'v': true, 'h': true, 'a': true, 'q': true} // applies to a nick, not the channel
	chanModeKeyParam   = map[byte]bool{'k': true}                                             // always takes a parameter
	chanModeLimitParam = map[byte]bool{'l': true}                                             // takes a parameter only when being set
)

// ApplyModeDelta merges an incremental "+ntk key" / "-l" style mode change
// (as received in a MODE message) into the buffer's tracked channel modes.
func (b *Buffer) ApplyModeDelta(modeline string, args []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.applyModeTokensLocked(modeline, args)
	b.Modes = b.renderModesLocked()
}

// SetModeState replaces the buffer's tracked channel modes wholesale, as
// received from a RPL_CHANNELMODEIS (324) reply.
func (b *Buffer) SetModeState(modeline string, args []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.modeFlags = map[byte]string{}
	b.applyModeTokensLocked(modeline, args)
	b.Modes = b.renderModesLocked()
}

// applyModeTokensLocked parses a mode string like "+ntk-l" against args,
// updating b.modeFlags. Caller must hold b.mu.
func (b *Buffer) applyModeTokensLocked(modeline string, args []string) {
	if b.modeFlags == nil {
		b.modeFlags = map[byte]string{}
	}
	adding := true
	argi := 0
	nextArg := func() string {
		if argi < len(args) {
			v := args[argi]
			argi++
			return v
		}
		return ""
	}
	for i := 0; i < len(modeline); i++ {
		c := modeline[i]
		switch {
		case c == '+':
			adding = true
		case c == '-':
			adding = false
		case chanModeListTypes[c]:
			nextArg() // ban/except/invex entries aren't channel-level flags
		case chanModeUserPrefix[c]:
			nextArg() // targets a nick, not the channel itself
		case chanModeKeyParam[c]:
			v := nextArg()
			if adding {
				b.modeFlags[c] = v
			} else {
				delete(b.modeFlags, c)
			}
		case chanModeLimitParam[c]:
			if adding {
				b.modeFlags[c] = nextArg()
			} else {
				delete(b.modeFlags, c)
			}
		default:
			if adding {
				b.modeFlags[c] = ""
			} else {
				delete(b.modeFlags, c)
			}
		}
	}
}

// renderModesLocked builds the displayed "+flags params" string from
// b.modeFlags. Caller must hold b.mu.
func (b *Buffer) renderModesLocked() string {
	if len(b.modeFlags) == 0 {
		return ""
	}
	letters := make([]byte, 0, len(b.modeFlags))
	for c := range b.modeFlags {
		letters = append(letters, c)
	}
	sort.Slice(letters, func(i, j int) bool { return letters[i] < letters[j] })
	var sb strings.Builder
	sb.WriteByte('+')
	var params []string
	for _, c := range letters {
		sb.WriteByte(c)
		if v := b.modeFlags[c]; v != "" {
			params = append(params, v)
		}
	}
	for _, p := range params {
		sb.WriteByte(' ')
		sb.WriteString(p)
	}
	return sb.String()
}
