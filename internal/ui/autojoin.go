package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rivo/tview"
)

// autojoinPath returns the path to the autojoin channel list file.
func autojoinPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "beacon", "autojoin")
}

// loadAutojoin reads the saved autojoin channel list (one channel per line).
// Returns nil if the file does not exist.
func loadAutojoin() []string {
	data, err := os.ReadFile(autojoinPath())
	if err != nil {
		return nil
	}
	var channels []string
	for _, line := range strings.Split(string(data), "\n") {
		ch := strings.TrimSpace(line)
		if ch != "" {
			channels = append(channels, ch)
		}
	}
	return channels
}

// saveAutojoin writes the autojoin channel list to disk (one channel per line).
func saveAutojoin(channels []string) error {
	p := autojoinPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	content := strings.Join(channels, "\n")
	if len(channels) > 0 {
		content += "\n"
	}
	return os.WriteFile(p, []byte(content), 0600)
}

// cmdAutojoin implements /autojoin [add|del|list] [#channel].
func (a *App) cmdAutojoin(args string) {
	sub, rest := splitTwo(args)
	switch strings.ToLower(sub) {
	case "add":
		ch := strings.TrimSpace(rest)
		if ch == "" {
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("usage: /autojoin add #channel")
				return
			}
			ch = b.Name
		}
		channels := loadAutojoin()
		for _, c := range channels {
			if strings.EqualFold(c, ch) {
				a.printlnInfo(tview.Escape(ch) + " is already in the autojoin list")
				return
			}
		}
		channels = append(channels, ch)
		if err := saveAutojoin(channels); err != nil {
			a.printlnError("autojoin: " + err.Error())
			return
		}
		a.printlnInfo("added " + tview.Escape(ch) + " to autojoin list")

	case "del", "remove":
		ch := strings.TrimSpace(rest)
		if ch == "" {
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("usage: /autojoin del #channel")
				return
			}
			ch = b.Name
		}
		channels := loadAutojoin()
		var kept []string
		found := false
		for _, c := range channels {
			if strings.EqualFold(c, ch) {
				found = true
				continue
			}
			kept = append(kept, c)
		}
		if !found {
			a.printlnInfo(tview.Escape(ch) + " is not in the autojoin list")
			return
		}
		if err := saveAutojoin(kept); err != nil {
			a.printlnError("autojoin: " + err.Error())
			return
		}
		a.printlnInfo("removed " + tview.Escape(ch) + " from autojoin list")

	case "list", "":
		channels := loadAutojoin()
		if len(channels) == 0 {
			a.printlnInfo("autojoin list is empty")
			return
		}
		a.printlnInfo("autojoin: " + strings.Join(channels, ", "))

	default:
		a.printlnError("usage: /autojoin [add|del|list] [#channel]")
	}
}
