package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"beacon/internal/theme"
)

// globalTSFmt is the live timestamp format used by fmtTime.
// Changed via SetTimestampFormat (called by the /set timestamp_format handler).
var (
	tsFmtMu     sync.RWMutex
	globalTSFmt = "15:04:05"
)

// SetTimestampFormat updates the live timestamp format string.
func SetTimestampFormat(f string) {
	tsFmtMu.Lock()
	globalTSFmt = f
	tsFmtMu.Unlock()
}

// mircColors maps mIRC color indices 0-15 to tview hex colors.
var mircColors = [16]string{
	"#ffffff", "#000000", "#00007f", "#009300",
	"#ff0000", "#7f0000", "#9c009c", "#fc7f00",
	"#ffff00", "#00fc00", "#009393", "#00ffff",
	"#0000fc", "#ff00ff", "#7f7f7f", "#d2d2d2",
}

// IRCFormat converts IRC text formatting bytes into tview color/style tags:
//
//	\x02  bold        \x1d  italic       \x1f  underline
//	\x16  reverse     \x0f  reset all
//	\x03[fg][,bg]  mIRC color (0-15); bg is ignored so text stays transparent
//
// Literal '[' characters are escaped to "[[]" so tview does not misinterpret
// them as color tags. The result must NOT be additionally passed through
// tview.Escape (that would double-escape the '[').
func IRCFormat(s string) string {
	s = expandEmojiCodes(s)

	type fmtState struct {
		fg, bg                 int
		bold, ital, under, rev bool
	}
	cur := fmtState{fg: -1, bg: -1}

	tag := func(st fmtState) string {
		fg := "-"
		if st.fg >= 0 && st.fg < 16 {
			fg = mircColors[st.fg]
		}
		bg := "-"
		attrs := ""
		if st.bold {
			attrs += "b"
		}
		if st.ital {
			attrs += "i"
		}
		if st.under {
			attrs += "u"
		}
		if st.rev {
			attrs += "r"
		}
		if attrs == "" {
			attrs = "-"
		}
		return fmt.Sprintf("[%s:%s:%s]", fg, bg, attrs)
	}

	var b strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		switch ch {
		case '\x03': // mIRC color
			i++
			fg, bg := -1, -1
			if i < len(s) && s[i] >= '0' && s[i] <= '9' {
				fg = int(s[i] - '0')
				i++
				if i < len(s) && s[i] >= '0' && s[i] <= '9' {
					fg = fg*10 + int(s[i]-'0')
					i++
				}
				if i < len(s) && s[i] == ',' {
					i++
					if i < len(s) && s[i] >= '0' && s[i] <= '9' {
						bg = int(s[i] - '0')
						i++
						if i < len(s) && s[i] >= '0' && s[i] <= '9' {
							bg = bg*10 + int(s[i]-'0')
							i++
						}
					}
				}
			}
			if fg > 15 {
				fg = 15
			}
			if bg > 15 {
				bg = 15
			}
			cur.fg, cur.bg = fg, bg
			b.WriteString(tag(cur))
		case '\x02': // bold
			cur.bold = !cur.bold
			b.WriteString(tag(cur))
			i++
		case '\x1d': // italic
			cur.ital = !cur.ital
			b.WriteString(tag(cur))
			i++
		case '\x1f': // underline
			cur.under = !cur.under
			b.WriteString(tag(cur))
			i++
		case '\x16': // reverse
			cur.rev = !cur.rev
			b.WriteString(tag(cur))
			i++
		case '\x0f': // reset all
			cur = fmtState{fg: -1, bg: -1}
			b.WriteString("[-:-:-]")
			i++
		case '[': // escape literal bracket so tview doesn't treat it as a tag
			b.WriteString("[[]")
			i++
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
}

// fmtTime returns a framed timestamp using the live timestamp_format setting.
func fmtTime(t time.Time) string {
	tsFmtMu.RLock()
	f := globalTSFmt
	tsFmtMu.RUnlock()
	return fmt.Sprintf("%s[%s%s%s]%s",
		theme.TimeFrame,
		theme.Time, t.Format(f), theme.TimeFrame,
		theme.Reset,
	)
}

// fmtNick colors a nick. Self-coloring is handled by the caller.
func fmtNick(nick, prefix string) string {
	color := theme.NickOther
	switch prefix {
	case "@", "&", "~":
		color = theme.NickOp
	case "+":
		color = theme.NickVoice
	}
	return color + tview.Escape(prefix+nick) + theme.Reset
}

// fmtSelfNick colors our own nick.
func fmtSelfNick(nick string) string {
	return theme.NickSelf + tview.Escape(nick) + theme.Reset
}

// line is the common prefix used for non-event message lines.
func line(t time.Time, body string) string {
	return fmt.Sprintf("%s %s\n", fmtTime(t), body)
}

// FormatPrivmsg renders a regular channel/query message as
//
//	[HH:MM:SS] [nick] hello there
func FormatPrivmsg(t time.Time, nick, prefix, text string, self, mention bool) string {
	var n string
	if self {
		n = fmtSelfNick(nick)
	} else {
		n = fmtNick(nick, prefix)
	}
	body := theme.Text + IRCFormat(text) + theme.Reset
	if mention {
		body = theme.NickOp + IRCFormat(text) + theme.Reset
	}
	br := theme.Bracket
	if self {
		br = theme.BracketSelf
	}
	return line(t, fmt.Sprintf("%s[%s%s] %s",
		br, n, br, body))
}

// FormatAction renders /me style action messages.
func FormatAction(t time.Time, nick, text string) string {
	return line(t, fmt.Sprintf("%s* %s %s%s",
		theme.Action,
		tview.Escape(nick),
		IRCFormat(text),
		theme.Reset,
	))
}

// FormatNotice renders a NOTICE.
func FormatNotice(t time.Time, from, target, text string) string {
	return line(t, fmt.Sprintf("%s-%s%s%s-%s:%s %s%s",
		theme.Notice,
		theme.NickOther, tview.Escape(from), theme.Notice,
		theme.BracketHi, tview.Escape(target),
		theme.Text, IRCFormat(text))+theme.Reset)
}

// FormatJoin renders a JOIN event.
func FormatJoin(t time.Time, nick, user, host, channel string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s (%s%s@%s%s) has joined %s%s%s",
		theme.EventJoin, theme.ArrowIn,
		theme.NickOther, tview.Escape(nick), theme.EventJoin,
		theme.Server, tview.Escape(user), tview.Escape(host), theme.EventJoin,
		theme.Channel, tview.Escape(channel), theme.Reset,
	))
}

// FormatPart renders a PART event.
func FormatPart(t time.Time, nick, user, host, channel, reason string) string {
	reasonPart := ""
	if reason != "" {
		reasonPart = fmt.Sprintf(" %s(%s%s%s)", theme.Bracket,
			theme.Text+IRCFormat(reason)+theme.EventPart, theme.Bracket, theme.EventPart)
	}
	return line(t, fmt.Sprintf("%s%s %s%s%s (%s%s@%s%s) has left %s%s%s%s",
		theme.EventPart, theme.ArrowOut,
		theme.NickOther, tview.Escape(nick), theme.EventPart,
		theme.Server, tview.Escape(user), tview.Escape(host), theme.EventPart,
		theme.Channel, tview.Escape(channel),
		reasonPart, theme.Reset,
	))
}

// FormatQuit renders a QUIT event.
func FormatQuit(t time.Time, nick, user, host, reason string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s (%s%s@%s%s) has quit %s(%s%s%s)%s",
		theme.EventQuit, theme.ArrowOut,
		theme.NickOther, tview.Escape(nick), theme.EventQuit,
		theme.Server, tview.Escape(user), tview.Escape(host), theme.EventQuit,
		theme.Bracket, theme.Text, IRCFormat(reason), theme.Bracket, theme.Reset,
	))
}

// FormatNick renders a NICK change.
func FormatNick(t time.Time, oldNick, newNick string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s is now known as %s%s%s",
		theme.EventNick, theme.Dash,
		theme.NickOther, tview.Escape(oldNick), theme.EventNick,
		theme.NickSelf, tview.Escape(newNick), theme.Reset,
	))
}

// FormatMode renders a MODE event.
func FormatMode(t time.Time, by, target, modes string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s sets mode %s%s%s on %s%s%s",
		theme.EventMode, theme.Star,
		theme.NickOther, tview.Escape(by), theme.EventMode,
		theme.Info, tview.Escape(modes), theme.EventMode,
		theme.Channel, tview.Escape(target), theme.Reset,
	))
}

// FormatTopic renders a TOPIC change.
func FormatTopic(t time.Time, by, channel, topic string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s changed the topic of %s%s%s to: %s%s%s",
		theme.Topic, "[T]",
		theme.NickOther, tview.Escape(by), theme.Topic,
		theme.Channel, tview.Escape(channel), theme.Topic,
		theme.Text, IRCFormat(topic), theme.Reset,
	))
}

// FormatKick renders a KICK.
func FormatKick(t time.Time, by, victim, channel, reason string) string {
	return line(t, fmt.Sprintf("%s%s %s%s%s was kicked from %s%s%s by %s%s%s (%s%s%s)",
		theme.EventKick, theme.Dash,
		theme.NickSelf, tview.Escape(victim), theme.EventKick,
		theme.Channel, tview.Escape(channel), theme.EventKick,
		theme.NickOther, tview.Escape(by), theme.EventKick,
		theme.Text, IRCFormat(reason), theme.Reset,
	))
}

// FormatServer renders a server numeric / generic server line.
func FormatServer(t time.Time, source, text string) string {
	return line(t, fmt.Sprintf("%s-%s%s%s-%s %s%s",
		theme.Numeric,
		theme.Server, tview.Escape(source), theme.Numeric,
		theme.Numeric,
		theme.Text, tview.Escape(text))+theme.Reset)
}

// FormatError renders an error line.
func FormatError(t time.Time, text string) string {
	return line(t, fmt.Sprintf("%s!! %s%s",
		theme.Error, tview.Escape(text), theme.Reset))
}

// FormatInfo renders a local informational line.
func FormatInfo(t time.Time, text string) string {
	return line(t, fmt.Sprintf("%s%s %s%s",
		theme.Info, theme.Dash, tview.Escape(text), theme.Reset))
}

// FormatCTCPRequest renders an inbound CTCP request (received via PRIVMSG).
//
//	[14:32:01] [CTCP] >>> from nick [VERSION]  optional-args
func FormatCTCPRequest(t time.Time, from, kind, args string) string {
	body := fmt.Sprintf("%s[%sCTCP%s]%s %s>>>%s from %s%s %s[%s%s%s]",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.EventJoin, theme.Reset,
		theme.NickOther, tview.Escape(from),
		theme.Bracket, theme.Info, tview.Escape(kind), theme.Bracket)
	if args != "" {
		body += fmt.Sprintf("  %s%s", theme.Text, tview.Escape(args))
	}
	return line(t, body+theme.Reset)
}

// FormatCTCPReply renders an inbound CTCP reply (received via NOTICE).
//
//	[14:32:01] [CTCP] <<< from nick [VERSION] : reply text
func FormatCTCPReply(t time.Time, from, kind, args string) string {
	body := fmt.Sprintf("%s[%sCTCP%s]%s %s<<<%s from %s%s %s[%s%s%s]",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.EventNick, theme.Reset,
		theme.NickOther, tview.Escape(from),
		theme.Bracket, theme.Info, tview.Escape(kind), theme.Bracket)
	if args != "" {
		body += fmt.Sprintf(" %s:%s %s%s",
			theme.Bracket, theme.Reset, theme.Text, tview.Escape(args))
	}
	return line(t, body+theme.Reset)
}

// FormatCTCPSent renders an outbound CTCP request we initiated.
//
//	[14:32:01] [CTCP] ->>> to nick [PING]  arg
func FormatCTCPSent(t time.Time, to, kind, args string) string {
	body := fmt.Sprintf("%s[%sCTCP%s]%s %s->>%s to %s%s %s[%s%s%s]",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.Action, theme.Reset,
		theme.NickSelf, tview.Escape(to),
		theme.Bracket, theme.Info, tview.Escape(kind), theme.Bracket)
	if args != "" {
		body += fmt.Sprintf("  %s%s", theme.Text, tview.Escape(args))
	}
	return line(t, body+theme.Reset)
}

// FormatCTCPPingReply renders a CTCP PING reply with computed RTT.
func FormatCTCPPingReply(t time.Time, from string, rtt time.Duration) string {
	return line(t, fmt.Sprintf(
		"%s[%sCTCP%s]%s %sPING reply%s from %s%s%s : %s%s%s",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.EventNick, theme.Reset,
		theme.NickOther, tview.Escape(from), theme.Reset,
		theme.WhoisAccent, fmtRTT(rtt), theme.Reset,
	))
}

func fmtRTT(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.3fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%dus", d.Microseconds())
	}
}

// ExtractCTCP scans a PRIVMSG/NOTICE payload for embedded CTCP segments.
// It returns the textual leftovers (joined with single spaces) and a list of
// (kind, args) pairs for each CTCP block. Per the CTCP spec a message may
// contain multiple \x01..\x01 sections interleaved with normal text.
func ExtractCTCP(s string) (text string, ctcps []CTCPSegment) {
	var plain strings.Builder
	for {
		i := strings.IndexByte(s, 0x01)
		if i < 0 {
			plain.WriteString(s)
			break
		}
		plain.WriteString(s[:i])
		s = s[i+1:]
		j := strings.IndexByte(s, 0x01)
		if j < 0 {
			// unterminated — treat the rest as plain text and bail
			plain.WriteByte(0x01)
			plain.WriteString(s)
			break
		}
		body := s[:j]
		s = s[j+1:]
		if body == "" {
			continue
		}
		parts := strings.SplitN(body, " ", 2)
		seg := CTCPSegment{Kind: strings.ToUpper(parts[0])}
		if len(parts) > 1 {
			seg.Args = parts[1]
		}
		ctcps = append(ctcps, seg)
	}
	return strings.TrimSpace(plain.String()), ctcps
}

// CTCPSegment is one parsed CTCP block from a PRIVMSG/NOTICE payload.
type CTCPSegment struct {
	Kind string
	Args string
}

// ---------------------------------------------------------------------------
// WHOIS block — pretty, framed, label-aligned decoration.
// ---------------------------------------------------------------------------

// whoisLabelWidth keeps every field cleanly aligned in the block.
const whoisLabelWidth = 10

// whoisFrame returns the start/end frame chars in the whois color.
func whoisFrame(s string) string {
	return theme.WhoisFrame + s + theme.Reset
}

// FormatWhoisStart returns the box-top header for a whois block.
//
//	╔══[ whois ]══[ nick ]══════════════════════════
func FormatWhoisStart(t time.Time, nick string) string {
	return line(t, fmt.Sprintf("%s %s %s%s%s %s %s%s%s %s",
		whoisFrame("╔══"),
		whoisFrame("["),
		theme.WhoisAccent, "whois", theme.Reset,
		whoisFrame("]══["),
		theme.WhoisNick, tview.Escape(nick), theme.Reset,
		whoisFrame("]══════════════════════════"),
	))
}

// FormatWhoisField renders one labeled field inside the whois block.
//
//	║  label    : value
func FormatWhoisField(t time.Time, label, value string) string {
	pad := label
	if len(pad) < whoisLabelWidth {
		pad = pad + strings.Repeat(" ", whoisLabelWidth-len(pad))
	}
	return line(t, fmt.Sprintf("%s %s%s%s %s:%s %s",
		whoisFrame("║"),
		theme.WhoisLabel, pad, theme.Reset,
		theme.WhoisFrame, theme.Reset,
		value, // caller is responsible for escaping & coloring the value
	))
}

// FormatWhoisNotFound renders a compact "no such nick" error in whois style.
//
//	╔══[ whois ]══[ nick ]══════════════════════════
//	║  no such nick
//	╚══[ end of whois ]════════════════════════════
func FormatWhoisNotFound(t time.Time, nick string) string {
	box := func(s string) string { return theme.WhoisFrame + s + theme.Reset }
	return line(t, fmt.Sprintf("%s %s %s%s%s %s %s%s%s %s",
		box("╔══"), box("["),
		theme.WhoisAccent, "whois", theme.Reset,
		box("]══["),
		theme.WhoisNick, tview.Escape(nick), theme.Reset,
		box("]══════════════════════════"),
	)) +
		line(t, fmt.Sprintf("%s %s%s%s",
			box("║"),
			theme.Error, "no such nick/channel", theme.Reset,
		))
	// No footer here — the server always follows 401 with 318 (RPL_ENDOFWHOIS)
	// which renders the closing ╚══ line via FormatWhoisEnd.
}

// FormatWhoisEnd returns the box-bottom footer for the whois block.
func FormatWhoisEnd(t time.Time, nick string) string {
	return line(t, fmt.Sprintf("%s %s%s end of whois %s%s",
		whoisFrame("╚══"),
		whoisFrame("["), theme.WhoisAccent,
		whoisFrame("]"),
		whoisFrame("══════════════════════════════════"),
	))
}

// WhoisValue colors a plain text whois value.
func WhoisValue(s string) string {
	return theme.WhoisValue + tview.Escape(s) + theme.Reset
}

// WhoisAccent colors a highlighted token (server name, account, "TLS", etc).
func WhoisAccent(s string) string {
	return theme.WhoisAccent + tview.Escape(s) + theme.Reset
}

// WhoisChannels colors a list of channels with their access prefixes,
// keeping @/+/&/~ in op/voice colors.
func WhoisChannels(list string) string {
	var b strings.Builder
	for i, ch := range strings.Fields(list) {
		if i > 0 {
			b.WriteString(" ")
		}
		prefix := ""
		name := ch
		if len(name) > 0 {
			switch name[0] {
			case '@', '&', '~':
				prefix = string(name[0])
				name = name[1:]
				b.WriteString(theme.NickOp + prefix + theme.Reset)
			case '+', '%':
				prefix = string(name[0])
				name = name[1:]
				b.WriteString(theme.NickVoice + prefix + theme.Reset)
			}
		}
		b.WriteString(theme.Channel + tview.Escape(name) + theme.Reset)
	}
	return b.String()
}

// HumanIdle renders an idle duration in seconds as a compact "1d 2h 3m 4s".
func HumanIdle(secs int64) string {
	if secs <= 0 {
		return "0s"
	}
	d := time.Duration(secs) * time.Second
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	out := ""
	if days > 0 {
		out += fmt.Sprintf("%dd ", days)
	}
	if days > 0 || hours > 0 {
		out += fmt.Sprintf("%dh ", hours)
	}
	if days > 0 || hours > 0 || mins > 0 {
		out += fmt.Sprintf("%dm ", mins)
	}
	out += fmt.Sprintf("%ds", int(d/time.Second))
	return out
}

// HumanSignon renders a unix timestamp string as "Mon Jan 02 15:04:05 2006".
// Returns empty string if the value can't be parsed.
func HumanSignon(unix string) string {
	if unix == "" {
		return ""
	}
	var n int64
	_, err := fmt.Sscanf(unix, "%d", &n)
	if err != nil || n <= 0 {
		return ""
	}
	return time.Unix(n, 0).Format("Mon Jan 02 15:04:05 2006")
}

// Mention reports whether the message text mentions our nick as a whole word.
func Mention(text, nick string) bool {
	if nick == "" {
		return false
	}
	lt := strings.ToLower(text)
	ln := strings.ToLower(nick)
	idx := 0
	for {
		i := strings.Index(lt[idx:], ln)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(ln)
		left := start == 0 || !isNickChar(lt[start-1])
		right := end == len(lt) || !isNickChar(lt[end])
		if left && right {
			return true
		}
		idx = end
	}
}

func isNickChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_' || b == '\\' || b == '[' || b == ']' || b == '{' || b == '}' || b == '|' || b == '`' || b == '^':
		return true
	}
	return false
}
