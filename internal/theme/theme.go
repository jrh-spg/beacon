// Package theme defines the BitchX-inspired color palette and decorations.
//
// The classic BitchX look leaned heavily on bracketed status fields, two-tone
// separators, and chunky ASCII arrows. We emulate that flavor here using tview
// color tags, which accept the form [fg:bg:flags].
package theme

import "fmt"

// Palette holds the named colors used throughout the UI. Anything outside
// these helpers should reach for tview.Escape on user-controlled strings.
//
// The colors below mirror the classic BitchX look: the bold, bright 16-color
// IRC palette leaning hard on cyan/blue chrome with hot-magenta accents,
// bold-white nicks, and saturated event colors.
var (
	// Chrome — cyan/blue structural chrome
	Bracket    = "[#B7E7FC:-:-]" // cyan-blue frame pieces (others)
	BracketSelf = "[#00ffff:-:-]" // bright cyan frame pieces (self)
	BracketHi  = "[#00ffff:-:-]" // bright cyan
	Frame     = "[#0000af:-:-]" // deep blue pipes/dashes
	Pipe      = "[#00afff:-:-]" // cyan separators
	Reset     = "[-:-:-]"      // tview "reset to default"

	// Timestamps — bright cyan clock, bright green brackets
	Time      = "[#C7C7C7:-:b]" // grey
	TimeFrame = "[#26C778:-:-]" // bright green brackets

	// Channels / chrome accents — bright pink
	Channel   = "[#FFD3F0:-:b]" // bright pink
	Topic     = "[#00ffff:-:-]" // bright cyan

	// Nicks — cyan-blue others, magenta ops, green voice, bold-white self
	NickSelf  = "[#ffffff:-:b]"
	NickOther = "[#B7E7FC:-:b]" // bold cyan-blue
	NickOp    = "[#FFD3F0:-:b]" // bright pink
	NickVoice = "[#26C778:-:-]" // bright green

	// Message text
	Text      = "[#c6c6c6:-:-]" // light grey
	Action    = "[#FFD3F0:-:-]" // /me — bright pink
	Notice    = "[#00ffff:-:b]" // bright cyan
	CTCP      = "[#B7E7FC:-:-]" // cyan-blue

	// Events — green joins, cyan-blue parts, blue quits
	EventJoin = "[#55ff55:-:-]" // bright green
	EventPart = "[#00afff:-:-]" // cyan-blue
	EventQuit = "[#0087ff:-:-]" // blue
	EventMode = "[#c6c6c6:-:-]" // light grey
	EventNick = "[#00ffff:-:-]" // bright cyan
	EventKick = "[#ff5555:-:b]" // bright red

	// Server / numerics / errors
	Server    = "[#ffffff:-:-]" // white server/source names
	Numeric   = "[#00afff:-:-]" // cyan-blue
	Error     = "[#ff5555:-:b]" // bright red
	Info      = "[#55ff55:-:-]" // bright green for local info lines

	// Whois block — cyan-blue frame, bright-cyan labels, magenta nick
	WhoisFrame  = "[#B7E7FC:-:-]" // cyan-blue border
	WhoisLabel  = "[#00ffff:-:b]" // bright cyan label
	WhoisValue  = "[#c6c6c6:-:-]"
	WhoisNick   = "[#FFD3F0:-:b]" // bright pink subject nick
	WhoisAccent = "[#00afff:-:-]" // cyan-blue accent
	WhoisAway   = "[#c6c6c6:-:-]" // light grey away message

	// Status bar — cyan/pink
	StatusBG    = ""
	StatusText  = "[#c6c6c6:-:-]"
	StatusTime  = "[#C49CE6:-:-]" // status bar clock
	StatusKey   = "[#00ffff:-:b]" // bright cyan key labels
	StatusChan  = "[#FFD3F0:-:b]" // bright pink channel names
	StatusAct   = "[#55ff55:-:-]" // bright green activity
	StatusHi    = "[#FFD3F0:-:b]" // bright pink highlight
	StatusBrack = "[#B7E7FC:-:-]" // cyan-blue brackets

	// Input prompt — bright cyan
	Prompt = "[#00ffff:-:b]"
)

// L wraps s with bracket frame: [ s ].
func L(s string) string {
	return Bracket + "[" + BracketHi + s + Bracket + "]" + Reset
}

// ArrowIn is the triple-chevron join marker, ramped bright to deep cyan.
const ArrowIn = "[#26C778:-:-]»[#B7E7FC:-:-]»[#FFD3F0:-:-]»[-:-:-]"

// ArrowOut is the triple-chevron part/quit marker, ramped deep to bright cyan.
const ArrowOut = "[#26C778:-:-]«[#B7E7FC:-:-]«[#FFD3F0:-:-]«[-:-:-]"

// Star is the mode/info marker.
const Star = "[*]"

// Dash is the generic event dash, BitchX-style.
const Dash = "-!-"

// Banner returns the startup splash text.
func Banner(version string) string {
	return fmt.Sprintf(
		"%s%s%s%s%s\n"+
			"%s   ____                              \n"+
			"%s  | __ )  ___  __ _  ___ ___  _ __  \n"+
			"%s  |  _ \\ / _ \\/ _` |/ __/ _ \\| '_ \\ \n"+
			"%s  | |_) |  __/ (_| | (_| (_) | | | |\n"+
			"%s  |____/ \\___|\\__,_|\\___\\___/|_| |_| %sv%s%s\n"+
			"%s  -=> a BitchX-inspired irc client <=-%s\n"+
			"%s%s%s%s%s\n",
		Frame, "==============================================", Reset, "", "",
		Numeric, Numeric, Numeric, Numeric, Numeric, Info, version, Reset,
		Action, Reset,
		Frame, "==============================================", Reset, "", "",
	)
}
