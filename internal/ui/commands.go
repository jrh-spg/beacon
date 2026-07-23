package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"beacon/internal/irc"
)

// runCommand parses a single slash command and dispatches it. The leading
// slash has already been stripped.
func (a *App) runCommand(line string) {
	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "help", "h", "?":
		a.cmdHelp()
	case "server", "connect":
		a.cmdServer(args, false)
	case "sslserver", "ssl":
		a.cmdServer(args, true)
	case "disconnect", "dc":
		reason := args
		if reason == "" {
			reason = "leaving"
		}
		a.disconnect(reason)
	case "quit", "exit":
		reason := args
		if reason == "" {
			reason = a.settings.Get("quit_message")
		}
		if reason == "" {
			reason = "beacon out — the rock burns clean"
		}
		a.disconnect(reason)
		a.tapp.Stop()
	case "nick":
		if args == "" {
			a.printlnError("usage: /nick <newnick>")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw("NICK " + args)
		}
	case "join", "j":
		if args == "" {
			a.printlnError("usage: /join <#channel> [key]")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw("JOIN " + args)
		}
	case "part", "leave":
		ch, reason := splitTwo(args)
		if ch == "" {
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("usage: /part [#channel] [reason]")
				return
			}
			ch = b.Name
		}
		if a.requireConn() {
			if reason == "" {
				reason = a.settings.Get("part_message")
			}
			if reason != "" {
				_ = a.conn.WriteRaw("PART " + ch + " :" + reason)
			} else {
				_ = a.conn.WriteRaw("PART " + ch)
			}
		}
	case "close", "wc":
		b := a.activeBuffer()
		if b.Kind == BufChannel && a.conn != nil && a.registered {
			_ = a.conn.WriteRaw("PART " + b.Name)
		}
		a.closeBuffer(b.Name)
	case "msg", "query":
		t, body := splitTwo(args)
		if t == "" || body == "" {
			a.printlnError("usage: /msg <target> <text>")
			return
		}
		body = expandEmojiCodes(body)
		if !a.requireConn() {
			return
		}
		_ = a.conn.WriteRaw(fmt.Sprintf("PRIVMSG %s :%s", t, body))
		var b *Buffer
		if irc.IsChannel(t) {
			b = a.findBuffer(t)
		} else {
			b = a.findBuffer(t)
			if b == nil {
				b = a.addBuffer(t, BufQuery)
			}
		}
		if b == nil {
			b = a.statusBuf()
		}
		a.writeRaw(b, FormatPrivmsg(time.Now(), a.curNick, "", body, true, false), ActLow)
	case "me", "action":
		if args == "" {
			a.printlnError("usage: /me <action>")
			return
		}
		args = expandEmojiCodes(args)
		b := a.activeBuffer()
		if b.Kind != BufChannel && b.Kind != BufQuery {
			a.printlnError("/me needs a channel or query")
			return
		}
		if !a.requireConn() {
			return
		}
		_ = a.conn.WriteRaw(fmt.Sprintf("PRIVMSG %s :\x01ACTION %s\x01", b.Name, args))
		a.writeRaw(b, FormatAction(time.Now(), a.curNick, args), ActLow)
	case "notice":
		t, body := splitTwo(args)
		if t == "" || body == "" {
			a.printlnError("usage: /notice <target> <text>")
			return
		}
		body = expandEmojiCodes(body)
		if !a.requireConn() {
			return
		}
		_ = a.conn.WriteRaw(fmt.Sprintf("NOTICE %s :%s", t, body))
		a.printlnInfo(fmt.Sprintf("-> -%s- %s", t, body))
	case "topic":
		ch, body := splitTwo(args)
		if ch == "" {
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("usage: /topic [#chan] <topic>")
				return
			}
			ch = b.Name
			body = strings.TrimSpace(args)
		} else if !irc.IsChannel(ch) {
			// first token wasn't a channel — treat all as topic for active
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("usage: /topic [#chan] <topic>")
				return
			}
			body = args
			ch = b.Name
		}
		if a.requireConn() {
			if body != "" {
				_ = a.conn.WriteRaw(fmt.Sprintf("TOPIC %s :%s", ch, body))
			} else {
				_ = a.conn.WriteRaw("TOPIC " + ch)
			}
		}
	case "mode":
		if args == "" {
			a.printlnError("usage: /mode <target> <modes>")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw("MODE " + args)
		}
	case "kick":
		if a.requireConn() {
			b := a.activeBuffer()
			if b.Kind != BufChannel {
				a.printlnError("/kick must be used in a channel")
				return
			}
			parts := strings.SplitN(args, " ", 2)
			if parts[0] == "" {
				a.printlnError("usage: /kick <nick> [reason]")
				return
			}
			reason := ""
			if len(parts) > 1 {
				reason = parts[1]
			}
			if reason != "" {
				_ = a.conn.WriteRaw(fmt.Sprintf("KICK %s %s :%s", b.Name, parts[0], reason))
			} else {
				_ = a.conn.WriteRaw(fmt.Sprintf("KICK %s %s", b.Name, parts[0]))
			}
		}
	case "whois":
		if args == "" {
			a.printlnError("usage: /whois <nick>")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw("WHOIS " + args)
		}
	case "names", "n":
		if a.requireConn() {
			target := args
			if target == "" {
				b := a.activeBuffer()
				if b.Kind == BufChannel {
					target = b.Name
				}
			}
			if target == "" {
				a.printlnError("usage: /names <#chan>")
				return
			}
			a.mu.Lock()
			a.namesRequested[strings.ToLower(target)] = struct{}{}
			a.mu.Unlock()
			_ = a.conn.WriteRaw("NAMES " + target)
		}
	case "list":
		if a.requireConn() {
			_ = a.conn.WriteRaw("LIST " + args)
		}
	case "away":
		if a.requireConn() {
			if args != "" {
				_ = a.conn.WriteRaw("AWAY :" + args)
			} else {
				_ = a.conn.WriteRaw("AWAY")
			}
		}
	case "raw", "quote":
		if args == "" {
			a.printlnError("usage: /raw <line>")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw(args)
		}
	case "window", "win":
		if args == "" {
			a.printlnError("usage: /window <n|name|next|prev>")
			return
		}
		switch args {
		case "next", "n":
			a.mu.Lock()
			next := (a.active + 1) % len(a.buffers)
			a.mu.Unlock()
			a.switchToIndex(next)
		case "prev", "p":
			a.mu.Lock()
			prev := (a.active - 1 + len(a.buffers)) % len(a.buffers)
			a.mu.Unlock()
			a.switchToIndex(prev)
		default:
			if n, err := strconv.Atoi(args); err == nil {
				a.switchToIndex(n - 1)
				return
			}
			if !a.switchToName(args) {
				a.printlnError("no such window: " + args)
			}
		}
	case "buffers", "windows", "list-windows":
		a.cmdBuffers()
	case "clear":
		tv := a.activeBuffer().View
		tv.Clear()
		tv.ScrollToEnd()
	case "ctcp":
		a.cmdCTCP(args)
	case "ping":
		a.cmdPing(args)
	case "op":
		a.cmdChanMode(args, "+o", true)
	case "deop":
		a.cmdChanMode(args, "-o", true)
	case "voice":
		a.cmdChanMode(args, "+v", true)
	case "devoice":
		a.cmdChanMode(args, "-v", true)
	case "ban":
		a.cmdChanMode(args, "+b", false)
	case "unban":
		a.cmdChanMode(args, "-b", false)
	case "invite":
		a.cmdInvite(args)
	case "cycle", "hop":
		a.cmdCycle(args)
	case "wallops":
		if args == "" {
			a.printlnError("usage: /wallops <msg>")
			return
		}
		if a.requireConn() {
			_ = a.conn.WriteRaw("WALLOPS :" + args)
		}
	case "who":
		if a.requireConn() {
			_ = a.conn.WriteRaw("WHO " + args)
		}
	case "echo":
		a.writeRaw(a.activeBuffer(), FormatInfo(time.Now(), args), ActLow)
	case "version":
		a.printlnInfo(fmt.Sprintf("beacon %s — a BitchX-inspired irc client", a.cfg.Version))
	case "uptime":
		a.printlnInfo("uptime: " + time.Since(a.startedAt).Truncate(time.Second).String())
	case "date", "time":
		a.printlnInfo(time.Now().Format(time.RFC1123))
	case "lastlog":
		a.cmdLastlog(args)
	case "eval":
		a.cmdEval(args)
	case "set":
		a.cmdSet(args)
	case "toggle":
		a.cmdToggle(args)
	case "ignore":
		a.cmdIgnore(args)
	case "autojoin":
		a.cmdAutojoin(args)
	case "dcc":
		a.cmdDCC(args)
	default:
		a.printlnError("unknown command: /" + cmd + "  (try /help)")
	}
}

func (a *App) requireConn() bool {
	a.connMu.Lock()
	defer a.connMu.Unlock()
	if a.conn == nil {
		a.printlnError("not connected — try /server <host> [+port]")
		return false
	}
	return true
}

func (a *App) cmdServer(args string, useTLS bool) {
	if args == "" {
		a.printlnError("usage: /server <host>[:port] [tls]")
		return
	}
	fields := strings.Fields(args)
	host := fields[0]
	for _, f := range fields[1:] {
		switch strings.ToLower(f) {
		case "tls", "ssl", "+tls", "+ssl":
			useTLS = true
		case "notls", "plain":
			useTLS = false
		}
	}
	// allow +port shorthand (SSL port marker like irssi)
	if i := strings.Index(host, ":+"); i != -1 {
		useTLS = true
		host = host[:i] + ":" + host[i+2:]
	} else if strings.HasPrefix(host, "+") {
		useTLS = true
		host = host[1:]
	}
	go a.connect(host, useTLS)
}

func (a *App) cmdBuffers() {
	// Snapshot under the lock; do NOT hold a.mu while calling write/print
	// helpers because they re-acquire it (would deadlock).
	a.mu.Lock()
	type bufSnap struct {
		idx    int
		name   string
		active bool
		kind   BufferKind
	}
	snap := make([]bufSnap, 0, len(a.buffers))
	for i, b := range a.buffers {
		snap = append(snap, bufSnap{idx: i + 1, name: b.Name, active: i == a.active, kind: b.Kind})
	}
	status := a.buffers[0]
	a.mu.Unlock()

	a.writeRaw(status, FormatInfo(time.Now(), "--- buffers ---"), ActLow)
	for _, s := range snap {
		marker := " "
		if s.active {
			marker = "*"
		}
		a.writeRaw(status,
			FormatInfo(time.Now(),
				fmt.Sprintf("%s %d  %s  (%s)", marker, s.idx, s.name, bufKindName(s.kind))),
			ActLow)
	}
}

func bufKindName(k BufferKind) string {
	switch k {
	case BufStatus:
		return "status"
	case BufServer:
		return "server"
	case BufChannel:
		return "channel"
	case BufQuery:
		return "query"
	}
	return "?"
}

func splitTwo(s string) (string, string) {
	p := strings.SplitN(s, " ", 2)
	if len(p) == 1 {
		return p[0], ""
	}
	return p[0], p[1]
}

func (a *App) cmdHelp() {
	lines := []string{
		"--- beacon commands ---",
		"/server <host>[:port] [tls]   connect to a server (alias /connect)",
		"/sslserver <host>[:port]      connect with TLS (alias /ssl)",
		"/disconnect [reason]          drop the current connection",
		"/quit [reason]                quit beacon",
		"/nick <name>                  change your nick",
		"/join <#chan> [key]           join a channel (alias /j)",
		"/part [#chan] [reason]        leave channel (alias /leave)",
		"/cycle [#chan]                part + rejoin (alias /hop)",
		"/close                        close the current window (alias /wc)",
		"/msg <target> <text>          send a private message (alias /query)",
		"/notice <target> <text>       send a NOTICE",
		"/me <action>                  send /me action",
		"/topic [#chan] <topic>        view or set topic",
		"/mode <target> <modes>        set modes",
		"/op <n>...                    +o (alias /deop, /voice, /devoice)",
		"/ban <mask>                   +b mask (alias /unban)",
		"/invite <nick> [#chan]        invite to channel",
		"/kick <nick> [reason]         kick someone (in current channel)",
		"/whois <nick>                 whois a user",
		"/who [target]                 WHO query",
		"/names [#chan]                list nicks",
		"/list [pattern]               LIST channels",
		"/wallops <text>               broadcast WALLOPS",
		"/away [msg]                   set / unset away",
		"/raw <line>                   send raw IRC line (alias /quote)",
		"/window <n|name|next|prev>    switch window (alias /win)",
		"/buffers                      list windows",
		"/clear                        clear current window",
		"/lastlog <substring>          search current window scrollback",
		"/echo <text>                  print local text in current window",
		"/eval <cmd>;<cmd>;...         run multiple commands separated by ;",
		"/ctcp <target> <type> [args]  send a CTCP request",
		"/ping <target>                CTCP PING with round-trip timing",
		"/version [target]             show beacon version or CTCP VERSION",
		"/uptime                       beacon uptime since launch",
		"/date | /time                 show local date/time",
		"/set [key [value]]            view / change runtime settings",
		"/toggle <key>                 toggle a boolean setting",
		"/ignore [add|del|list] [nick] manage the ignore list",
		"/autojoin [add|del|list] [#chan]  manage auto-joined channels (saved to disk)",
		"/dcc list|send|chat|accept|close  direct client-to-client transfers",
		"--- keys ---",
		"PgUp/PgDn  scroll      Home/End  jump to top / re-enable autoscroll",
		"Ctrl+N/Ctrl+P  next/prev window  Alt+1..9  jump to window N",
		"Tab  command + nick + emoji completion   Up/Down  input history",
	}
	for _, l := range lines {
		a.writeRaw(a.statusBuf(), FormatInfo(time.Now(), l), ActLow)
	}
}

// cmdCTCP implements /ctcp <target> <type> [args]. The TYPE is forced to
// upper case to match the protocol convention.
func (a *App) cmdCTCP(args string) {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		a.printlnError("usage: /ctcp <target> <type> [args]")
		return
	}
	target := parts[0]
	kind := strings.ToUpper(parts[1])
	rest := ""
	if len(parts) > 2 {
		rest = parts[2]
	}
	if !a.requireConn() {
		return
	}
	if err := a.ctcpRequest(target, kind, rest); err != nil {
		a.printlnError("ctcp: " + err.Error())
	}
}

// cmdPing implements /ping <target> by sending CTCP PING with the current
// millisecond timestamp embedded as the argument. The reply handler in
// dispatch computes RTT when the value parses cleanly.
func (a *App) cmdPing(args string) {
	target := strings.TrimSpace(args)
	if target == "" {
		a.printlnError("usage: /ping <target>")
		return
	}
	if !a.requireConn() {
		return
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := a.ctcpRequest(target, "PING", ts); err != nil {
		a.printlnError("ping: " + err.Error())
	}
}

// cmdChanMode is the shared backend for /op /deop /voice /devoice /ban
// /unban. When nickArgs is true the args list is treated as a stream of
// nicks; otherwise it is passed through as MODE arguments (useful for
// ban masks).
func (a *App) cmdChanMode(args, modes string, nickArgs bool) {
	b := a.activeBuffer()
	if b.Kind != BufChannel {
		a.printlnError("must be used in a channel")
		return
	}
	args = strings.TrimSpace(args)
	if args == "" {
		a.printlnError("usage: /" + strings.TrimLeft(modes, "+-") + " <nick|mask> [<nick|mask>...]")
		return
	}
	if !a.requireConn() {
		return
	}
	targets := strings.Fields(args)
	if nickArgs {
		// Send one MODE per batch of up to 4 targets (most servers allow this).
		const batch = 4
		op := string(modes[0])
		letter := modes[1:]
		for i := 0; i < len(targets); i += batch {
			j := i + batch
			if j > len(targets) {
				j = len(targets)
			}
			n := j - i
			modeStr := op + strings.Repeat(letter, n)
			_ = a.conn.WriteRaw(fmt.Sprintf("MODE %s %s %s",
				b.Name, modeStr, strings.Join(targets[i:j], " ")))
		}
		return
	}
	// Plain arg passthrough (single mode letter, single mask per call).
	for _, t := range targets {
		_ = a.conn.WriteRaw(fmt.Sprintf("MODE %s %s %s", b.Name, modes, t))
	}
}

// cmdInvite implements /invite <nick> [#chan].
func (a *App) cmdInvite(args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		a.printlnError("usage: /invite <nick> [#chan]")
		return
	}
	nick := parts[0]
	ch := ""
	if len(parts) > 1 {
		ch = parts[1]
	} else {
		b := a.activeBuffer()
		if b.Kind != BufChannel {
			a.printlnError("no channel — supply one explicitly")
			return
		}
		ch = b.Name
	}
	if a.requireConn() {
		_ = a.conn.WriteRaw(fmt.Sprintf("INVITE %s %s", nick, ch))
	}
}

// cmdCycle implements /cycle [#chan] — part and immediately rejoin.
func (a *App) cmdCycle(args string) {
	ch := strings.TrimSpace(args)
	if ch == "" {
		b := a.activeBuffer()
		if b.Kind != BufChannel {
			a.printlnError("usage: /cycle [#chan]")
			return
		}
		ch = b.Name
	}
	if !a.requireConn() {
		return
	}
	_ = a.conn.WriteRaw("PART " + ch + " :cycling")
	_ = a.conn.WriteRaw("JOIN " + ch)
}

// cmdLastlog searches the active buffer's text for a pattern (case-insensitive
// substring) and echoes the matching lines to the same buffer with a marker.
func (a *App) cmdLastlog(args string) {
	pat := strings.TrimSpace(args)
	if pat == "" {
		a.printlnError("usage: /lastlog <pattern>")
		return
	}
	b := a.activeBuffer()
	body := b.View.GetText(true) // stripColors=true
	needle := strings.ToLower(pat)
	hits := 0
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), needle) {
			a.writeRaw(b, FormatInfo(time.Now(), "lastlog: "+line), ActLow)
			hits++
		}
	}
	a.printlnInfo(fmt.Sprintf("lastlog %q: %d hit(s)", pat, hits))
}

// cmdEval runs a sequence of slash commands separated by ";". Lines without
// a leading slash are sent as ordinary messages to the active buffer.
func (a *App) cmdEval(args string) {
	for _, raw := range strings.Split(args, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "/") {
			a.runCommand(strings.TrimPrefix(raw, "/"))
		} else {
			a.sendToActive(raw)
		}
	}
}
