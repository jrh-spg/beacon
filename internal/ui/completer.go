package ui

import (
	"sort"
	"strings"

	"beacon/internal/irc"
)

// completerState remembers the last tab-completion cycle so pressing Tab
// repeatedly walks through the candidate list.
type completerState struct {
	base    string   // unchanged input up to and including the leading space
	suffix  string   // appended after the candidate (": ", " ", "")
	matches []string // candidates
	idx     int      // current match
}

// allCommands is the canonical list used for /<command> completion. It is
// kept sorted so cycling order is deterministic.
var allCommands = func() []string {
	cmds := []string{
		"server", "connect", "sslserver", "ssl", "disconnect", "dc",
		"quit", "exit",
		"nick", "join", "j", "part", "leave", "close", "wc",
		"msg", "query", "notice", "me", "action",
		"topic", "mode", "kick", "whois", "names", "list", "away",
		"raw", "quote",
		"window", "win", "buffers", "windows", "list-windows",
		"clear", "ctcp", "ping",
		"op", "deop", "voice", "devoice", "ban", "unban",
		"invite", "cycle", "hop", "wallops", "who",
		"echo", "version", "uptime", "lastlog", "eval",
		"set", "toggle", "ignore",
		"dcc",
		"help", "h",
	}
	sort.Strings(cmds)
	return cmds
}()

// commandsTakingNick lists slash commands whose first argument is a nick.
// Used to drive nick completion at "/cmd <Tab>".
var commandsTakingNick = map[string]bool{
	"msg": true, "query": true, "whois": true, "ctcp": true, "ping": true,
	"kick": true, "op": true, "deop": true, "voice": true, "devoice": true,
	"ban": true, "unban": true, "invite": true, "ignore": true, "notice": true,
}

// commandsTakingWindow lists commands whose first argument is a window
// name or number.
var commandsTakingWindow = map[string]bool{
	"window": true, "win": true, "close": true, "wc": true,
}

// completeForInput inspects the current input and returns the unchanged
// "base" prefix, the suffix to append after the candidate, and a slice of
// candidate strings sorted alphabetically (case-insensitive).
func (a *App) completeForInput(text string) (base, suffix string, matches []string) {
	if text == "" {
		return "", "", nil
	}

	// Split into "base + token" where token is the trailing run after the
	// last space.
	lastSp := strings.LastIndexByte(text, ' ')
	token := text[lastSp+1:]

	// /command name completion (cursor is in the very first token and it
	// starts with "/").
	if lastSp == -1 && strings.HasPrefix(text, "/") {
		prefix := strings.ToLower(strings.TrimPrefix(token, "/"))
		for _, c := range allCommands {
			if strings.HasPrefix(c, prefix) {
				matches = append(matches, c)
			}
		}
		return "/", " ", matches
	}

	// /command <Tab> argument completion.
	if strings.HasPrefix(text, "/") {
		// First non-slash word is the command name.
		head := text
		if i := strings.IndexByte(head, ' '); i >= 0 {
			head = head[:i]
		}
		cmd := strings.ToLower(strings.TrimPrefix(head, "/"))

		base = text[:lastSp+1]
		prefix := strings.ToLower(token)

		if commandsTakingNick[cmd] {
			return base, " ", completeNicks(prefix, a.activeBuffer())
		}
		if commandsTakingWindow[cmd] {
			return base, "", a.completeWindows(prefix)
		}
		// Fall through to default nick completion.
		return base, " ", completeNicks(prefix, a.activeBuffer())
	}

	// Plain text in a channel/query — nick completion.
	suffix = " "
	if lastSp == -1 {
		// At the start of a fresh line follow the irssi convention of
		// appending the completion_char (default ":") after the name.
		ch := a.settings.Get("completion_char")
		if ch == "" {
			ch = ":"
		}
		suffix = ch + " "
	}
	base = text[:lastSp+1]
	return base, suffix, completeNicks(strings.ToLower(token), a.activeBuffer())
}

// completeNicks returns nicks in the buffer matching the prefix (case
// insensitive). Channel mode prefixes (@/+ etc.) are stripped for
// comparison and not returned.
func completeNicks(prefix string, b *Buffer) []string {
	if b == nil || b.Kind != BufChannel {
		return nil
	}
	var out []string
	for _, full := range b.NickList() {
		n := full
		if len(n) > 0 {
			switch n[0] {
			case '@', '+', '&', '~', '%':
				n = n[1:]
			}
		}
		if strings.HasPrefix(strings.ToLower(n), prefix) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

// completeWindows returns matching buffer names plus their numeric indices.
func (a *App) completeWindows(prefix string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, b := range a.buffers {
		if strings.HasPrefix(strings.ToLower(b.Name), prefix) {
			out = append(out, b.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

// tabComplete is wired to the Tab key from the InputField. It tracks state
// across consecutive presses so the user can cycle through matches.
func (a *App) tabComplete() {
	text := a.input.GetText()

	// Cycle within the previous match set if the user hasn't typed since
	// the last Tab.
	if a.comp.matches != nil && len(a.comp.matches) > 0 {
		expected := a.comp.base + a.comp.matches[a.comp.idx] + a.comp.suffix
		if text == expected {
			a.comp.idx = (a.comp.idx + 1) % len(a.comp.matches)
			a.input.SetText(a.comp.base + a.comp.matches[a.comp.idx] + a.comp.suffix)
			return
		}
	}

	base, suffix, matches := a.completeForInput(text)
	if len(matches) == 0 {
		a.comp = completerState{}
		return
	}
	a.comp = completerState{base: base, suffix: suffix, matches: matches, idx: 0}
	a.input.SetText(base + matches[0] + suffix)
}

// silence unused warning in builds that don't reference irc directly here
var _ = irc.IsChannel
