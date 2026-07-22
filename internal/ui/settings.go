package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sType is the value type for a setting definition.
type sType int

const (
	sBool   sType = iota
	sString       // arbitrary text
	sInt          // whole number
)

func (t sType) String() string {
	switch t {
	case sBool:
		return "bool"
	case sInt:
		return "int"
	default:
		return "string"
	}
}

// settingDef carries static metadata for one setting.
type settingDef struct {
	Key      string
	Type     sType
	Default  string
	Category string
	Desc     string
}

// allSettingDefs is the authoritative ordered list of supported settings,
// modelled closely on irssi's equivalent categories.
var allSettingDefs = []settingDef{
	// ── core ──────────────────────────────────────────────────────────────
	{"auto_reconnect", sBool, "true", "core",
		"Automatically reconnect after an unexpected disconnection"},
	{"reconnect_time", sInt, "5", "core",
		"Seconds to wait between reconnect attempts"},
	{"quit_message", sString, "leaving", "core",
		"Default QUIT message when none is given on /quit"},
	{"part_message", sString, "", "core",
		"Default PART message when none is given on /part"},

	// ── irc ───────────────────────────────────────────────────────────────
	{"alternate_nick", sString, "", "irc",
		"Fallback nick when the primary nick is already taken"},
	{"userinfo", sString, "beacon IRC client", "irc",
		"CTCP USERINFO reply"},
	{"ctcp_version_reply", sString, "", "irc",
		"Custom CTCP VERSION reply (empty = built-in beacon/version string)"},
	{"max_kicks", sInt, "1", "irc",
		"Maximum nicks to include in a single KICK command"},
	{"max_modes", sInt, "4", "irc",
		"Maximum mode changes to include in a single MODE command"},
	{"lag_check_time", sInt, "60", "irc",
		"Seconds between server PING lag checks (0 = disabled)"},

	// ── ui ────────────────────────────────────────────────────────────────
	{"timestamp_format", sString, "15:04:05", "ui",
		"Timestamp format string (Go time layout, e.g. 15:04 or 15:04:05)"},
	{"show_names_on_join", sBool, "true", "ui",
		"Show channel nick list automatically when joining a channel"},
	{"show_nickmode", sBool, "true", "ui",
		"Show @/+ mode prefix on nicks in channel messages"},
	{"hilight_nick_matches_everywhere", sBool, "false", "ui",
		"Highlight your nick when it appears anywhere in a message, not just the start"},
	{"window_history_lines", sInt, "5000", "ui",
		"Maximum number of lines to keep in each buffer's scrollback"},

	// ── completion ────────────────────────────────────────────────────────
	{"completion_char", sString, ":", "completion",
		"Character appended after a nick-completed name at the start of a line"},
	{"completion_auto", sBool, "false", "completion",
		"Automatically accept a completion when only one candidate matches"},

	// ── activity ──────────────────────────────────────────────────────────
	{"highlight_color", sString, "red", "activity",
		"tview color name used to paint highlighted (mention) messages"},
	{"beep_msg", sBool, "false", "activity",
		"Ring the terminal bell when a new message arrives in an inactive window"},
	{"auto_rejoin_on_kick", sBool, "false", "activity",
		"Automatically rejoin a channel after being kicked from it"},	{"show_join_parts", sBool, "true", "activity",
		"Show join, part, and quit events in channel buffers"},
	// ── dcc ───────────────────────────────────────────────────────────────
	{"dcc_auto_accept", sBool, "false", "dcc",
		"Automatically accept incoming DCC file offers without asking"},
	{"dcc_download_dir", sString, "", "dcc",
		"Directory for files received via DCC SEND (empty = ~/Downloads)"},
	{"dcc_timeout", sInt, "120", "dcc",
		"Seconds to wait for a remote DCC connection before giving up"},
	{"dcc_port", sInt, "0", "dcc",
		"Fixed TCP port for outgoing DCC offers (0 = kernel-assigned ephemeral)"},

	// ── log ───────────────────────────────────────────────────────────────
	{"log_enabled", sBool, "false", "log",
		"Write channel and query traffic to log files automatically"},
	{"log_dir", sString, "", "log",
		"Directory for log files (empty = ~/irclogs)"},
	{"log_timestamp", sString, "15:04:05", "log",
		"Timestamp format used inside log files"},
}

// categoryOrder defines display order for /set output.
var categoryOrder = []string{"core", "irc", "ui", "completion", "activity", "dcc", "log"}

// ----------------------------------------------------------------------------
// settingsStore
// ----------------------------------------------------------------------------

// settingsStore is a thread-safe key/value store backed by a definition table.
type settingsStore struct {
	mu   sync.RWMutex
	vals map[string]string        // current values (defaults pre-populated)
	defs map[string]*settingDef   // fast lookup by key
}

func newSettings() *settingsStore {
	home, _ := os.UserHomeDir()
	s := &settingsStore{
		vals: map[string]string{},
		defs: map[string]*settingDef{},
	}
	for i := range allSettingDefs {
		d := &allSettingDefs[i]
		s.defs[d.Key] = d
		def := d.Default
		// Expand empty dir defaults at init time.
		if d.Key == "dcc_download_dir" && def == "" {
			def = filepath.Join(home, "Downloads")
		}
		if d.Key == "log_dir" && def == "" {
			def = filepath.Join(home, "irclogs")
		}
		if def != "" {
			s.vals[d.Key] = def
		}
	}
	return s
}

// Get returns the current string value for key (empty string if unset).
func (s *settingsStore) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vals[key]
}

// Bool returns the boolean interpretation of a setting.
func (s *settingsStore) Bool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(s.Get(key)))
	return v == "true" || v == "yes" || v == "on" || v == "1"
}

// Set validates value against the definition type and stores it.
// Returns an error string if validation fails, empty on success.
func (s *settingsStore) Set(key, value string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.defs[key]
	if d != nil {
		switch d.Type {
		case sBool:
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "true", "yes", "on", "1", "false", "no", "off", "0":
				// normalise
				if strings.ToLower(strings.TrimSpace(value)) == "on" ||
					strings.ToLower(strings.TrimSpace(value)) == "yes" ||
					strings.ToLower(strings.TrimSpace(value)) == "1" {
					value = "true"
				} else if strings.ToLower(strings.TrimSpace(value)) == "off" ||
					strings.ToLower(strings.TrimSpace(value)) == "no" ||
					strings.ToLower(strings.TrimSpace(value)) == "0" {
					value = "false"
				}
			default:
				return fmt.Sprintf("%s is a boolean setting; use ON or OFF", key)
			}
		case sInt:
			if _, err := strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return fmt.Sprintf("%s requires an integer value", key)
			}
			value = strings.TrimSpace(value)
		}
	}
	if value == "" {
		delete(s.vals, key)
	} else {
		s.vals[key] = value
	}
	return ""
}

// Toggle flips a boolean setting and returns its new value.
func (s *settingsStore) Toggle(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := strings.ToLower(strings.TrimSpace(s.vals[key]))
	cur := v == "true" || v == "yes" || v == "on" || v == "1"
	next := !cur
	if next {
		s.vals[key] = "true"
	} else {
		s.vals[key] = "false"
	}
	return next
}

// All returns a sorted snapshot of all key/value pairs.
func (s *settingsStore) All() [][2]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([][2]string, 0, len(s.vals))
	for k, v := range s.vals {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// def returns the definition for key, or nil.
func (s *settingsStore) def(key string) *settingDef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defs[key]
}

// ignoreStore is a case-insensitive set of nicks whose messages should be
// dropped on the floor by dispatch.
type ignoreStore struct {
	mu    sync.RWMutex
	nicks map[string]struct{}
}

func newIgnore() *ignoreStore {
	return &ignoreStore{nicks: map[string]struct{}{}}
}

func (i *ignoreStore) Add(nick string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.nicks[strings.ToLower(nick)] = struct{}{}
}

func (i *ignoreStore) Remove(nick string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.nicks, strings.ToLower(nick))
}

func (i *ignoreStore) Has(nick string) bool {
	if nick == "" {
		return false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	_, ok := i.nicks[strings.ToLower(nick)]
	return ok
}

func (i *ignoreStore) List() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]string, 0, len(i.nicks))
	for n := range i.nicks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ----------------------------------------------------------------------------
// Command handlers
// ----------------------------------------------------------------------------

// cmdSet implements /set [category|key [value]].
//
// /set                   — list every setting grouped by category
// /set <category>        — list only the settings in that category
// /set <key>             — show current value + type + description
// /set <key> <value>     — change a setting (type-validated)
func (a *App) cmdSet(args string) {
	args = strings.TrimSpace(args)

	// /set  →  show all settings grouped by category
	if args == "" {
		a.printSettingsByCategory("")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	key := parts[0]

	// Check if the argument names a category.
	for _, cat := range categoryOrder {
		if strings.EqualFold(key, cat) {
			a.printSettingsByCategory(cat)
			return
		}
	}

	// /set <key>  →  show one setting
	if len(parts) == 1 {
		a.printOneSetting(key)
		return
	}

	// /set <key> <value>  →  change a setting
	val := parts[1]
	if errMsg := a.settings.Set(key, val); errMsg != "" {
		a.printlnError(errMsg)
		return
	}
	// Apply side effects for settings that drive live behaviour.
	a.applySettingSideEffect(key, a.settings.Get(key))
	d := a.settings.def(key)
	typeStr := sString.String()
	if d != nil {
		typeStr = d.Type.String()
	}
	a.printlnInfo(fmt.Sprintf("%-30s = %s  (%s)", key, a.settings.Get(key), typeStr))
}

// printSettingsByCategory prints all known settings, optionally filtered to
// one category, in a format similar to irssi's /set output.
func (a *App) printSettingsByCategory(filterCat string) {
	cur := ""
	for _, cat := range categoryOrder {
		if filterCat != "" && !strings.EqualFold(cat, filterCat) {
			continue
		}
		for i := range allSettingDefs {
			d := &allSettingDefs[i]
			if d.Category != cat {
				continue
			}
			if cur != cat {
				cur = cat
				a.printlnInfo(fmt.Sprintf("[ %s ]", cat))
			}
			val := a.settings.Get(d.Key)
			disp := val
			if disp == "" {
				disp = "(unset)"
			}
			a.printlnInfo(fmt.Sprintf("  %-32s %s", d.Key, disp))
		}
	}
}

// printOneSetting shows full metadata for a single key.
func (a *App) printOneSetting(key string) {
	d := a.settings.def(key)
	val := a.settings.Get(key)
	if d == nil {
		if val == "" {
			a.printlnError(fmt.Sprintf("unknown setting: %s", key))
			return
		}
		// User-defined key with no definition.
		a.printlnInfo(fmt.Sprintf("%s = %s", key, val))
		return
	}
	disp := val
	if disp == "" {
		disp = "(unset)"
	}
	a.printlnInfo(fmt.Sprintf("%s = %s  [%s, %s]", key, disp, d.Type, d.Category))
	a.printlnInfo(fmt.Sprintf("  %s", d.Desc))
}

// applySettingSideEffect wires live settings changes into running subsystems.
func (a *App) applySettingSideEffect(key, val string) {
	switch key {
	case "timestamp_format":
		if val != "" {
			SetTimestampFormat(val)
		}
	case "window_history_lines":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			a.mu.Lock()
			for _, b := range a.buffers {
				b.View.SetMaxLines(n)
			}
			a.mu.Unlock()
		}
	}
}

func (a *App) cmdToggle(args string) {
	key := strings.TrimSpace(args)
	if key == "" {
		a.printlnError("usage: /toggle <key>")
		return
	}
	d := a.settings.def(key)
	if d != nil && d.Type != sBool {
		a.printlnError(fmt.Sprintf("%s is not a boolean setting (use /set %s <value>)", key, key))
		return
	}
	v := a.settings.Toggle(key)
	status := "OFF"
	if v {
		status = "ON"
	}
	a.printlnInfo(fmt.Sprintf("%-30s = %s", key, status))
	a.applySettingSideEffect(key, a.settings.Get(key))
}

func (a *App) cmdIgnore(args string) {
	args = strings.TrimSpace(args)
	if args == "" || strings.EqualFold(args, "list") {
		nicks := a.ignore.List()
		if len(nicks) == 0 {
			a.printlnInfo("ignore: (empty)")
			return
		}
		a.printlnInfo("ignore: " + strings.Join(nicks, " "))
		return
	}
	parts := strings.Fields(args)
	verb := strings.ToLower(parts[0])
	switch verb {
	case "add":
		for _, n := range parts[1:] {
			a.ignore.Add(n)
		}
		a.printlnInfo("ignored: " + strings.Join(parts[1:], " "))
	case "del", "remove", "rm":
		for _, n := range parts[1:] {
			a.ignore.Remove(n)
		}
		a.printlnInfo("unignored: " + strings.Join(parts[1:], " "))
	default:
		// /ignore nick — treat as add
		for _, n := range parts {
			a.ignore.Add(n)
		}
		a.printlnInfo("ignored: " + strings.Join(parts, " "))
	}
}

// keep "time" import referenced even if it's added/removed during edits
var _ = time.Now
