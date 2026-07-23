package ui

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"beacon/internal/irc"
	"beacon/internal/theme"
)

// Config carries connection defaults sourced from the CLI.
type Config struct {
	Server        string // host[:port]
	UseTLS        bool
	TLSSkipVerify bool
	Nick          string
	AltNick       string
	User          string
	Realname      string
	Password      string
	AutoJoin      []string
	AutoConnect   bool
	Version       string
	// SASL authentication
	SASLMechanism string // "EXTERNAL" or "PLAIN"
	SASLUser      string // PLAIN only
	SASLPass      string // PLAIN only
	// TLS client certificate (for SASL EXTERNAL / CertFP)
	CertFile string
	KeyFile  string
}

// App ties together the TUI, buffer list, and IRC connection.
type App struct {
	cfg Config

	tapp   *tview.Application
	pages  *tview.Pages
	root   *tview.Flex
	status *tview.TextView
	title  *tview.TextView
	input  *tview.InputField

	mu      sync.Mutex
	buffers []*Buffer
	active  int
	history []string
	histPos int
	comp    completerState

	connMu     sync.Mutex
	conn       *irc.Conn
	registered bool
	curNick    string
	serverName string

	// runtime tunables and user-curated ignore list
	settings  *settingsStore
	ignore    *ignoreStore
	startedAt time.Time

	// dcc holds pending offers and live transfers/chats
	dcc *dccState

	// namesRequested tracks channels for which the user explicitly typed
	// /names; the 366 handler only prints the list for those channels.
	namesRequested map[string]struct{}

	// drawReq is a 1-deep buffered channel used to coalesce redraw
	// requests; the drawer goroutine debounces them so a flood of writes
	// can't saturate tcell's event queue.
	drawReq  chan struct{}
	stopDraw chan struct{}
}

// New builds a fully wired Application.
func New(cfg Config) *App {
	a := &App{
		cfg:       cfg,
		curNick:   cfg.Nick,
		drawReq:   make(chan struct{}, 1),
		stopDraw:  make(chan struct{}),
		settings:  newSettings(),
		ignore:    newIgnore(),
		startedAt: time.Now(),
	}
	a.dcc = newDCCState()
	a.namesRequested = map[string]struct{}{}
	a.tapp = tview.NewApplication()
	a.tapp.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		s.Clear()
		return false
	})

	a.title = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetScrollable(false)
	a.title.SetBackgroundColor(tcell.ColorDefault)

	a.status = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetScrollable(false)
	a.status.SetBackgroundColor(tcell.ColorDefault)

	a.input = tview.NewInputField().
		SetLabel("[#00ffff:-:-][beacon] [#26C778:-:-]»[#B7E7FC:-:-]»[#FFD3F0:-:-]»[-:-:-] ").
		SetLabelStyle(tcell.StyleDefault.Background(tcell.ColorDefault)).
		SetFieldStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDefault)).
		SetPlaceholderStyle(tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorDefault)).
		SetFieldWidth(0)
	a.input.SetBackgroundColor(tcell.ColorDefault)
	a.input.SetFormAttributes(0, tcell.ColorDefault, tcell.ColorDefault, tcell.ColorWhite, tcell.ColorDefault)
	a.input.SetDoneFunc(a.handleInputDone)

	a.pages = tview.NewPages()
	a.pages.SetBackgroundColor(tcell.ColorDefault)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.title, 1, 0, false).
		AddItem(a.pages, 0, 1, false).
		AddItem(a.status, 3, 0, false).
		AddItem(a.input, 1, 0, true)
	a.root.SetBackgroundColor(tcell.ColorDefault)

	a.tapp.SetRoot(a.root, true)
	a.tapp.SetInputCapture(a.handleKey)

	// status buffer always exists, and must be the visible page from t=0
	status := a.addBuffer("(status)", BufStatus)
	a.active = 0
	a.pages.SwitchToPage(status.Name)
	fmt.Fprint(status.View, theme.Banner(cfg.Version))
	a.printlnInfo("welcome to beacon — type /help for commands")
	a.refreshTitle()

	return a
}

// Run starts the TUI. Blocks until the user quits.
func (a *App) Run() error {
	go a.drawLoop()
	if a.cfg.AutoConnect && a.cfg.Server != "" {
		go func() {
			// brief settle so the UI is up before we draw connect lines
			time.Sleep(50 * time.Millisecond)
			a.connect(a.cfg.Server, a.cfg.UseTLS)
		}()
	}
	// 1Hz status repaint to keep the clock current without flooding draws
	go a.statusTicker()
	a.refreshStatus()
	return a.tapp.Run()
}

// Stop gracefully shuts the app down.
func (a *App) Stop() {
	a.disconnect("client exiting")
	close(a.stopDraw)
	a.tapp.Stop()
}

// drawLoop coalesces redraw requests at ~30fps so high-traffic channels
// can't lock up the input loop by overwhelming tcell's event queue.
func (a *App) drawLoop() {
	const minInterval = 33 * time.Millisecond
	for {
		select {
		case <-a.stopDraw:
			return
		case <-a.drawReq:
			a.tapp.Draw()
			// drain any extra request that arrived while drawing, then sleep
			select {
			case <-a.drawReq:
			default:
			}
			time.Sleep(minInterval)
		}
	}
}

// requestDraw asks the drawer goroutine to repaint. Safe from any goroutine
// and never blocks.
func (a *App) requestDraw() {
	select {
	case a.drawReq <- struct{}{}:
	default:
	}
}

// statusTicker keeps the clock in the status bar ticking once per second.
func (a *App) statusTicker() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.stopDraw:
			return
		case <-t.C:
			a.refreshStatus()
		}
	}
}

// ----------------------------------------------------------------------------
// Buffer management
// ----------------------------------------------------------------------------

func (a *App) addBuffer(name string, kind BufferKind) *Buffer {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.buffers {
		if strings.EqualFold(b.Name, name) {
			return b
		}
	}
	b := NewBuffer(name, kind)
	b.View.SetChangedFunc(func() { a.requestDraw() })
	a.buffers = append(a.buffers, b)
	a.pages.AddPage(name, b.View, true, false)
	return b
}

func (a *App) findBuffer(name string) *Buffer {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.buffers {
		if strings.EqualFold(b.Name, name) {
			return b
		}
	}
	return nil
}

func (a *App) statusBuf() *Buffer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.buffers[0]
}

func (a *App) activeBuffer() *Buffer {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active < 0 || a.active >= len(a.buffers) {
		return a.buffers[0]
	}
	return a.buffers[a.active]
}

func (a *App) switchToIndex(i int) {
	a.mu.Lock()
	if i < 0 || i >= len(a.buffers) {
		a.mu.Unlock()
		return
	}
	a.active = i
	b := a.buffers[i]
	b.Activity = ActNone
	a.mu.Unlock()
	a.pages.SwitchToPage(b.Name)
	a.refreshTitle()
	a.refreshStatus()
}

func (a *App) switchToName(name string) bool {
	a.mu.Lock()
	for i, b := range a.buffers {
		if strings.EqualFold(b.Name, name) {
			a.mu.Unlock()
			a.switchToIndex(i)
			return true
		}
	}
	a.mu.Unlock()
	return false
}

func (a *App) closeBuffer(name string) {
	a.mu.Lock()
	if strings.EqualFold(name, "(status)") {
		a.mu.Unlock()
		return
	}
	for i, b := range a.buffers {
		if strings.EqualFold(b.Name, name) {
			a.buffers = append(a.buffers[:i], a.buffers[i+1:]...)
			a.pages.RemovePage(b.Name)
			if a.active >= len(a.buffers) {
				a.active = len(a.buffers) - 1
			}
			b2 := a.buffers[a.active]
			a.mu.Unlock()
			a.pages.SwitchToPage(b2.Name)
			a.refreshTitle()
			a.refreshStatus()
			return
		}
	}
	a.mu.Unlock()
}

// ----------------------------------------------------------------------------
// Writing into buffers
// ----------------------------------------------------------------------------

func (a *App) writeRaw(target *Buffer, text string, act ActivityLevel) {
	if target == nil {
		target = a.statusBuf()
	}
	// The TextView's ChangedFunc schedules a debounced redraw, so this
	// write is cheap even under heavy traffic.
	fmt.Fprint(target.View, text)
	a.mu.Lock()
	activityChanged := false
	if a.buffers[a.active] != target && act > target.Activity {
		target.Activity = act
		activityChanged = true
	}
	a.mu.Unlock()
	if activityChanged {
		a.refreshStatus()
	}
}

func (a *App) printlnInfo(text string) {
	a.writeRaw(a.statusBuf(), FormatInfo(time.Now(), text), ActLow)
}

func (a *App) printlnError(text string) {
	a.writeRaw(a.activeBuffer(), FormatError(time.Now(), text), ActMsg)
}

// ----------------------------------------------------------------------------
// Status / title bars
// ----------------------------------------------------------------------------

func (a *App) refreshTitle() {
	b := a.activeBuffer()
	var s string
	switch b.Kind {
	case BufChannel:
		topic := IRCFormat(b.Topic)
		if topic == "" {
			topic = "(no topic set)"
		}
		s = fmt.Sprintf("%s [T]%s %s%s %s| %s%s",
			theme.Topic, theme.Reset,
			theme.Channel, tview.Escape(b.Name),
			theme.Pipe,
			theme.Text, topic)
	case BufQuery:
		s = fmt.Sprintf(" %s[query]%s %s%s",
			theme.Action, theme.Reset, theme.NickSelf, tview.Escape(b.Name))
	case BufServer:
		s = fmt.Sprintf(" %s[server]%s %s%s",
			theme.Server, theme.Reset, theme.Info, tview.Escape(b.Name))
	default:
		s = fmt.Sprintf(" %s[beacon status]%s", theme.Info, theme.Reset)
	}
	a.title.SetText(s)
	a.requestDraw()
}

func (a *App) refreshStatus() {
	a.mu.Lock()
	parts := []string{}
	parts = append(parts,
		fmt.Sprintf("%s[%s%s%s]", theme.StatusBrack, theme.StatusTime, time.Now().Format("15:04"), theme.StatusBrack))
	nickShow := a.curNick
	if nickShow == "" {
		nickShow = "*"
	}
	connTag := "disc"
	if a.registered {
		connTag = "conn"
	}
	parts = append(parts,
		fmt.Sprintf("%s[%s%s%s/%s%s%s]", theme.StatusBrack,
			theme.StatusText, tview.Escape(nickShow), theme.StatusBrack,
			theme.StatusKey, connTag, theme.StatusBrack))

	for i, b := range a.buffers {
		marker := ""
		switch {
		case i == a.active:
			marker = fmt.Sprintf("%s>%s", theme.StatusHi, theme.StatusBrack)
		case b.Activity == ActHilite:
			marker = fmt.Sprintf("%s!%s", theme.StatusHi, theme.StatusBrack)
		case b.Activity == ActMsg:
			marker = fmt.Sprintf("%s+%s", theme.StatusAct, theme.StatusBrack)
		case b.Activity == ActLow:
			marker = fmt.Sprintf("%s.%s", theme.StatusText, theme.StatusBrack)
		default:
			marker = fmt.Sprintf("%s-%s", theme.StatusBrack, theme.StatusBrack)
		}
		nameColor := theme.StatusText
		if b.Kind == BufChannel {
			nameColor = theme.StatusChan
		}
		modes := ""
		if b.Modes != "" {
			modes = fmt.Sprintf("(%s)", tview.Escape(b.Modes))
		}
		parts = append(parts,
			fmt.Sprintf("%s[%s%d:%s%s%s%s%s]", theme.StatusBrack,
				marker, i+1,
				nameColor, tview.Escape(b.Name), theme.StatusBrack,
				modes, theme.StatusBrack))
	}
	a.mu.Unlock()
	statusLine := strings.Join(parts, " ")
	_, _, statusWidth, _ := a.status.GetInnerRect()
	pad := func(left string, right string) string {
		spaces := statusWidth - tview.TaggedStringWidth(left) - tview.TaggedStringWidth(right)
		if spaces < 1 {
			spaces = 1
		}
		return strings.Repeat(" ", spaces)
	}
	topLeft := fmt.Sprintf("%s ▄▄%s", theme.StatusBrack, theme.Reset)
	middleLeft := fmt.Sprintf("%s█%s %s", theme.StatusBrack, theme.Reset, statusLine)
	bottomLeft := fmt.Sprintf("%s·▀ ▀%s", theme.StatusBrack, theme.Reset)
	topRight := fmt.Sprintf("%s▄· %s", theme.StatusBrack, theme.Reset)
	middleRight := fmt.Sprintf("%s  █%s", theme.StatusBrack, theme.Reset)
	bottomRight := fmt.Sprintf("%s▀▀ %s", theme.StatusBrack, theme.Reset)
	a.status.SetText(fmt.Sprintf("%s%s%s\n%s%s%s\n%s%s%s",
		topLeft, pad(topLeft, topRight), topRight,
		middleLeft, pad(middleLeft, middleRight), middleRight,
		bottomLeft, pad(bottomLeft, bottomRight), bottomRight))
	a.requestDraw()
}

// ----------------------------------------------------------------------------
// Key/input handling
// ----------------------------------------------------------------------------

func (a *App) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyCtrlC:
		// Swallow Ctrl+C so tview's default handler can't stop the app.
		// Quitting is done explicitly via /quit.
		return nil
	case tcell.KeyPgUp:
		tv := a.activeBuffer().View
		_, _, _, h := tv.GetInnerRect()
		if h < 1 {
			h = 1
		}
		row, _ := tv.GetScrollOffset()
		// page up = h-1 lines so we keep one line of overlap, like irssi
		next := row - (h - 1)
		if next < 0 {
			next = 0
		}
		tv.ScrollTo(next, 0)
		return nil
	case tcell.KeyPgDn:
		tv := a.activeBuffer().View
		_, _, _, h := tv.GetInnerRect()
		if h < 1 {
			h = 1
		}
		row, _ := tv.GetScrollOffset()
		total := tv.GetWrappedLineCount()
		next := row + (h - 1)
		// If we're scrolling at or past the end, re-enable autoscroll
		// instead of just jumping there (otherwise tview disables it).
		if next >= total-h {
			tv.ScrollToEnd()
		} else {
			tv.ScrollTo(next, 0)
		}
		return nil
	case tcell.KeyCtrlN:
		a.mu.Lock()
		next := (a.active + 1) % len(a.buffers)
		a.mu.Unlock()
		a.switchToIndex(next)
		return nil
	case tcell.KeyCtrlP:
		a.mu.Lock()
		prev := (a.active - 1 + len(a.buffers)) % len(a.buffers)
		a.mu.Unlock()
		a.switchToIndex(prev)
		return nil
	case tcell.KeyUp:
		if a.histPos > 0 {
			a.histPos--
			a.input.SetText(a.history[a.histPos])
		}
		return nil
	case tcell.KeyDown:
		if a.histPos < len(a.history)-1 {
			a.histPos++
			a.input.SetText(a.history[a.histPos])
		} else if a.histPos == len(a.history)-1 {
			a.histPos = len(a.history)
			a.input.SetText("")
		}
		return nil
	case tcell.KeyTab:
		a.tabComplete()
		return nil
	}
	if ev.Modifiers()&tcell.ModAlt != 0 && ev.Key() == tcell.KeyRune {
		r := ev.Rune()
		if r >= '1' && r <= '9' {
			a.switchToIndex(int(r - '1'))
			return nil
		}
	}
	return ev
}

func (a *App) handleInputDone(key tcell.Key) {
	if key != tcell.KeyEnter {
		return
	}
	line := a.input.GetText()
	a.input.SetText("")
	if line == "" {
		return
	}
	a.history = append(a.history, line)
	a.histPos = len(a.history)

	if strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "//") {
		a.runCommand(line[1:])
		return
	}
	if strings.HasPrefix(line, "//") {
		line = line[1:]
	}
	a.sendToActive(line)
}

func (a *App) sendToActive(text string) {
	text = expandEmojiCodes(text)
	b := a.activeBuffer()
	if b.Kind == BufDCC {
		// DCC chat buffers are named "=peer". Strip the prefix and write
		// to the live DCC chat socket instead of the IRC connection.
		peer := strings.TrimPrefix(b.Name, "=")
		if !a.dccSendChat(peer, text) {
			a.printlnError("dcc chat not connected")
		}
		return
	}
	if b.Kind != BufChannel && b.Kind != BufQuery {
		a.printlnError("no target — use /msg <nick> <text> or /join #channel")
		return
	}
	if a.conn == nil || !a.registered {
		a.printlnError("not connected")
		return
	}
	if err := a.conn.WriteRaw(fmt.Sprintf("PRIVMSG %s :%s", b.Name, text)); err != nil {
		a.printlnError("send failed: " + err.Error())
		return
	}
	a.writeRaw(b, FormatPrivmsg(time.Now(), a.curNick, "", text, true, false), ActLow)
}

// ----------------------------------------------------------------------------
// Connection lifecycle
// ----------------------------------------------------------------------------

// connect dials and starts the read loop. Safe to call from any goroutine.
func (a *App) connect(addr string, useTLS bool) {
	a.disconnect("reconnecting")

	addr = normalizeAddr(addr, useTLS)
	a.printlnInfo(fmt.Sprintf("connecting to %s (tls=%v) ...", addr, useTLS))

	c, err := irc.Dial(irc.DialOptions{
		Addr:          addr,
		UseTLS:        useTLS,
		TLSSkipVerify: a.cfg.TLSSkipVerify,
		TLSCertFile:   a.cfg.CertFile,
		TLSKeyFile:    a.cfg.KeyFile,
		Timeout:       30 * time.Second,
	})
	if err != nil {
		a.printlnError("connect failed: " + err.Error())
		return
	}
	a.connMu.Lock()
	a.conn = c
	a.serverName = addr
	a.registered = false
	a.connMu.Unlock()

	// CAP / SASL negotiation must start before NICK/USER so the server
	// holds off sending 001 until we finish authenticating.
	if a.cfg.SASLMechanism != "" {
		_ = c.WriteRaw("CAP REQ :sasl")
	}
	// register
	if a.cfg.Password != "" {
		_ = c.WriteRaw("PASS " + a.cfg.Password)
	}
	_ = c.WriteRaw("NICK " + a.cfg.Nick)
	_ = c.WriteRaw(fmt.Sprintf("USER %s 0 * :%s", a.cfg.User, a.cfg.Realname))
	a.curNick = a.cfg.Nick

	go a.readLoop(c)
}

func (a *App) disconnect(reason string) {
	a.connMu.Lock()
	c := a.conn
	a.conn = nil
	a.registered = false
	a.connMu.Unlock()
	if c != nil {
		_ = c.WriteRaw("QUIT :" + reason)
		_ = c.Close()
		a.printlnInfo("disconnected: " + reason)
		a.refreshStatus()
	}
}

// readLoop pumps IRC messages until the connection closes.
func (a *App) readLoop(c *irc.Conn) {
	for {
		msg, err := c.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				a.printlnInfo("connection closed")
			} else {
				// only show error if this conn is still our active one
				a.connMu.Lock()
				active := a.conn == c
				a.connMu.Unlock()
				if active {
					a.printlnError("read: " + err.Error())
				}
			}
			a.connMu.Lock()
			if a.conn == c {
				a.conn = nil
				a.registered = false
			}
			a.connMu.Unlock()
			a.refreshStatus()
			return
		}
		a.dispatch(msg)
	}
}

func normalizeAddr(addr string, useTLS bool) string {
	if strings.Contains(addr, ":") {
		// host:port already; or maybe a v6 - leave it
		if _, _, err := net.SplitHostPort(addr); err == nil {
			return addr
		}
	}
	port := "6667"
	if useTLS {
		port = "6697"
	}
	return addr + ":" + port
}

// dispatch translates an inbound IRC message into UI updates.
func (a *App) dispatch(m *irc.Message) {
	now := time.Now()
	switch m.Command {
	case "PING":
		_ = a.conn.WriteRaw("PONG :" + m.Trailing())
	case "PRIVMSG":
		a.onPrivmsg(now, m, false)
	case "NOTICE":
		a.onPrivmsg(now, m, true)
	case "JOIN":
		ch := m.Target()
		if ch == "" {
			ch = m.Trailing()
		}
		if strings.EqualFold(m.Nick, a.curNick) {
			b := a.addBuffer(ch, BufChannel)
			a.switchToName(b.Name)
		}
		if b := a.findBuffer(ch); b != nil {
			b.AddNick(m.Nick, "")
			if a.settings.Bool("show_join_parts") {
				a.writeRaw(b, FormatJoin(now, m.Nick, m.User, m.Host, ch), ActLow)
			}
		}
	case "PART":
		ch := m.Target()
		reason := ""
		if len(m.Params) > 1 {
			reason = m.Params[len(m.Params)-1]
		}
		if b := a.findBuffer(ch); b != nil {
			b.RemoveNick(m.Nick)
			if a.settings.Bool("show_join_parts") {
				a.writeRaw(b, FormatPart(now, m.Nick, m.User, m.Host, ch, reason), ActLow)
			}
			if strings.EqualFold(m.Nick, a.curNick) {
				a.closeBuffer(ch)
			}
		}
	case "QUIT":
		reason := m.Trailing()
		a.mu.Lock()
		bufs := append([]*Buffer(nil), a.buffers...)
		a.mu.Unlock()
		for _, b := range bufs {
			// Use the buffer's own mutex via RemoveNick rather than
			// reading b.Nicks unlocked (data race).
			if b.HasNick(m.Nick) {
				b.RemoveNick(m.Nick)
				if a.settings.Bool("show_join_parts") {
					a.writeRaw(b, FormatQuit(now, m.Nick, m.User, m.Host, reason), ActLow)
				}
			}
		}
	case "NICK":
		newNick := m.Trailing()
		if strings.EqualFold(m.Nick, a.curNick) {
			a.curNick = newNick
		}
		a.mu.Lock()
		bufs := append([]*Buffer(nil), a.buffers...)
		a.mu.Unlock()
		for _, b := range bufs {
			if b.RenameNick(m.Nick, newNick) {
				a.writeRaw(b, FormatNick(now, m.Nick, newNick), ActLow)
			}
		}
	case "MODE":
		target := m.Target()
		modes := strings.Join(m.Params[1:], " ")
		if b := a.findBuffer(target); b != nil {
			a.writeRaw(b, FormatMode(now, m.Nick, target, modes), ActLow)
		} else {
			a.writeRaw(a.statusBuf(), FormatMode(now, m.Nick, target, modes), ActLow)
		}
	case "TOPIC":
		ch := m.Target()
		topic := m.Trailing()
		if b := a.findBuffer(ch); b != nil {
			b.Topic = topic
			a.writeRaw(b, FormatTopic(now, m.Nick, ch, topic), ActLow)
			if a.activeBuffer() == b {
				a.refreshTitle()
			}
		}
	case "KICK":
		ch := m.Target()
		victim := ""
		if len(m.Params) > 1 {
			victim = m.Params[1]
		}
		reason := ""
		if len(m.Params) > 2 {
			reason = m.Params[2]
		}
		if b := a.findBuffer(ch); b != nil {
			b.RemoveNick(victim)
			a.writeRaw(b, FormatKick(now, m.Nick, victim, ch, reason), ActMsg)
			if strings.EqualFold(victim, a.curNick) {
				a.printlnInfo("you were kicked from " + ch)
				if a.settings.Bool("auto_rejoin_on_kick") {
					_ = a.conn.WriteRaw("JOIN " + ch)
				}
			}
		}
	case "ERROR":
		a.printlnError("server: " + m.Trailing())
	case "001": // RPL_WELCOME
		a.registered = true
		if m.Target() != "" {
			a.curNick = m.Target()
		}
		a.writeRaw(a.statusBuf(), FormatServer(now, m.Prefix, m.Trailing()), ActLow)
		a.refreshStatus()
		for _, ch := range a.cfg.AutoJoin {
			_ = a.conn.WriteRaw("JOIN " + ch)
		}
		for _, ch := range loadAutojoin() {
			_ = a.conn.WriteRaw("JOIN " + ch)
		}
	case "332": // RPL_TOPIC
		if len(m.Params) >= 3 {
			ch := m.Params[1]
			topic := m.Params[2]
			if b := a.findBuffer(ch); b != nil {
				b.Topic = topic
				// IRCFormat the topic so embedded color codes render; the
				// channel name is safe to tview.Escape normally.
				msg := line(now, fmt.Sprintf("%s%s topic for %s%s%s: %s%s",
					theme.Info, theme.Dash,
					theme.Channel, tview.Escape(ch), theme.Info,
					IRCFormat(topic), theme.Reset))
				a.writeRaw(b, msg, ActLow)
				if a.activeBuffer() == b {
					a.refreshTitle()
				}
			}
		}
	case "353": // RPL_NAMREPLY
		if len(m.Params) >= 4 {
			ch := m.Params[2]
			names := strings.Fields(m.Params[3])
			if b := a.findBuffer(ch); b != nil {
				for _, n := range names {
					prefix := ""
					if len(n) > 0 {
						switch n[0] {
						case '@', '+', '&', '~', '%':
							prefix = string(n[0])
							n = n[1:]
						}
					}
					b.AddNick(n, prefix)
				}
			}
		}
	case "366": // RPL_ENDOFNAMES
		if len(m.Params) >= 2 {
			ch := m.Params[1]
			if b := a.findBuffer(ch); b != nil {
				// Only display the list when the user explicitly requested it.
				a.mu.Lock()
				_, requested := a.namesRequested[strings.ToLower(ch)]
				delete(a.namesRequested, strings.ToLower(ch))
				a.mu.Unlock()
				// Display unless both conditions say no: not explicitly requested AND setting is off.
				if !requested && !a.settings.Bool("show_names_on_join") {
					break
				}
				nicks := b.NickList()
				// Build a colored nick list: ops (@) in op color, voiced (+)
				// in voice color, everyone else in text color.
				var parts []string
				for _, pn := range nicks {
					var color string
					switch {
					case len(pn) > 0 && (pn[0] == '@' || pn[0] == '&' || pn[0] == '~'):
						color = theme.NickOp
					case len(pn) > 0 && (pn[0] == '+' || pn[0] == '%'):
						color = theme.NickVoice
					default:
						color = theme.Text
					}
					parts = append(parts, color+tview.Escape(pn)+theme.Reset)
				}
				a.writeRaw(b, FormatInfo(now, fmt.Sprintf("%d nicks in %s:", len(nicks), ch)), ActLow)
				// Print in rows of 8 so the buffer doesn't become one huge line.
				for i := 0; i < len(parts); i += 8 {
					end := i + 8
					if end > len(parts) {
						end = len(parts)
					}
					row := strings.Join(parts[i:end], " ")
					a.writeRaw(b, line(now, "  "+row), ActLow)
				}
			}
		}
	case "433": // ERR_NICKNAMEINUSE
		a.printlnError("nickname in use")
		if !a.registered && a.cfg.AltNick != "" && a.cfg.AltNick != a.curNick {
			a.curNick = a.cfg.AltNick
			_ = a.conn.WriteRaw("NICK " + a.cfg.AltNick)
		}
	// ── IRCv3 CAP / SASL ─────────────────────────────────────────────────
	case "CAP":
		// CAP <target> <subcommand> :<capabilities>
		if len(m.Params) < 2 {
			break
		}
		switch strings.ToUpper(m.Params[1]) {
		case "ACK":
			caps := strings.Fields(m.Trailing())
			for _, cap := range caps {
				if strings.EqualFold(cap, "sasl") {
					mech := strings.ToUpper(a.cfg.SASLMechanism)
					if mech == "" {
						mech = "EXTERNAL"
					}
					a.printlnInfo("SASL: starting " + mech + " authentication")
					_ = a.conn.WriteRaw("AUTHENTICATE " + mech)
				}
			}
		case "NAK":
			a.printlnError("SASL: server rejected capability request — aborting")
			_ = a.conn.WriteRaw("CAP END")
		}
	case "AUTHENTICATE":
		// Server is ready for our credentials (params[0] == "+").
		if len(m.Params) < 1 || m.Params[0] != "+" {
			break
		}
		switch strings.ToUpper(a.cfg.SASLMechanism) {
		case "EXTERNAL", "":
			// EXTERNAL: server uses the TLS client certificate fingerprint.
			// An empty payload ("+") tells it to use the presented cert.
			_ = a.conn.WriteRaw("AUTHENTICATE +")
		case "PLAIN":
			payload := saslPlainPayload(a.cfg.SASLUser, a.cfg.SASLPass)
			_ = a.conn.WriteRaw("AUTHENTICATE " + payload)
		default:
			a.printlnError("SASL: unknown mechanism " + a.cfg.SASLMechanism)
			_ = a.conn.WriteRaw("AUTHENTICATE *") // abort
		}
	case "900": // RPL_LOGGEDIN
		acct := ""
		if len(m.Params) >= 3 {
			acct = m.Params[2]
		}
		a.printlnInfo(fmt.Sprintf("SASL: logged in as %s%s",
			theme.WhoisNick, tview.Escape(acct)) + theme.Reset)
	case "903": // RPL_SASLSUCCESS
		a.printlnInfo("SASL: authentication successful")
		_ = a.conn.WriteRaw("CAP END")
	case "904", "905": // ERR_SASLFAIL / ERR_SASLTOOLONG
		a.printlnError("SASL authentication failed: " + m.Trailing())
		_ = a.conn.WriteRaw("CAP END")
	case "906": // ERR_SASLABORTED
		a.printlnError("SASL: authentication aborted")
	case "401", "406": // ERR_NOSUCHNICK / ERR_WASNOSUCHNICK
		// These arrive after /whois <nick> when the nick doesn't exist.
		// Render a compact whois-style error instead of dumping raw params.
		nick := ""
		if len(m.Params) >= 2 {
			nick = m.Params[1]
		} else if len(m.Params) == 1 {
			nick = m.Params[0]
		}
		if nick == "" {
			nick = m.Trailing()
		}
		b := a.whoisTarget(nick)
		a.writeRaw(b, FormatWhoisNotFound(now, nick), ActMsg)
	case "311", "312", "313", "317", "318", "319", "301",
		"330", "338", "378", "379", "671", "275", "307", "320":
		a.onWhois(now, m)
	default:
		// numeric or unhandled — dump to status
		text := strings.Join(m.Params, " ")
		a.writeRaw(a.statusBuf(), FormatServer(now, m.Prefix+" "+m.Command, text), ActLow)
	}
}

// whoisTarget picks the best buffer to render a whois block into: an existing
// query window for that nick if any, otherwise the currently-active buffer.
func (a *App) whoisTarget(nick string) *Buffer {
	if b := a.findBuffer(nick); b != nil {
		return b
	}
	return a.activeBuffer()
}

// onWhois renders any of the WHOIS-related numerics with framed decoration.
// The block is bracketed by 311 (start) and 318 (end); intermediate numerics
// emit a single labeled field row.
func (a *App) onWhois(now time.Time, m *irc.Message) {
	// All whois numerics have <me> as params[0] and <nick> as params[1].
	if len(m.Params) < 2 {
		a.writeRaw(a.statusBuf(),
			FormatServer(now, m.Prefix+" "+m.Command, strings.Join(m.Params, " ")),
			ActLow)
		return
	}
	nick := m.Params[1]
	b := a.whoisTarget(nick)

	field := func(label, value string) {
		a.writeRaw(b, FormatWhoisField(now, label, value), ActLow)
	}

	switch m.Command {
	case "311": // RPL_WHOISUSER  <nick> <user> <host> * :<realname>
		a.writeRaw(b, FormatWhoisStart(now, nick), ActLow)
		if len(m.Params) >= 5 {
			user, host := m.Params[2], m.Params[3]
			real := m.Trailing()
			field("ident",
				fmt.Sprintf("%s%s%s@%s%s%s",
					theme.WhoisAccent, tview.Escape(user), theme.Reset,
					theme.Server, tview.Escape(host), theme.Reset))
			field("name", WhoisValue(real))
		}
	case "312": // RPL_WHOISSERVER <nick> <server> :<info>
		if len(m.Params) >= 3 {
			server := m.Params[2]
			info := m.Trailing()
			field("server",
				fmt.Sprintf("%s  %s(%s%s%s)",
					WhoisAccent(server),
					theme.Bracket, theme.WhoisValue, tview.Escape(info), theme.Bracket)+theme.Reset)
		}
	case "313": // RPL_WHOISOPERATOR
		field("oper", WhoisAccent(strings.TrimSpace(m.Trailing())))
	case "317": // RPL_WHOISIDLE <nick> <secs> [<signon>] :<text>
		var secs int64
		if len(m.Params) >= 3 {
			fmt.Sscanf(m.Params[2], "%d", &secs)
		}
		signon := ""
		if len(m.Params) >= 5 {
			signon = HumanSignon(m.Params[3])
		}
		idle := HumanIdle(secs)
		val := WhoisAccent(idle)
		if signon != "" {
			val += fmt.Sprintf("  %s(signon %s%s%s)%s",
				theme.Bracket, theme.WhoisValue, tview.Escape(signon),
				theme.Bracket, theme.Reset)
		}
		field("idle", val)
	case "318": // RPL_ENDOFWHOIS
		a.writeRaw(b, FormatWhoisEnd(now, nick), ActLow)
	case "319": // RPL_WHOISCHANNELS
		field("channels", WhoisChannels(m.Trailing()))
	case "301": // RPL_AWAY
		field("away", theme.WhoisAway+tview.Escape(m.Trailing())+theme.Reset)
	case "330": // RPL_WHOISACCOUNT <nick> <account> :is logged in as
		if len(m.Params) >= 3 {
			field("account", WhoisAccent(m.Params[2]))
		}
	case "338": // RPL_WHOISACTUALLY  varies by network
		// Common shape: <nick> <ip> :Actual user@host
		val := WhoisValue(m.Trailing())
		if len(m.Params) >= 3 {
			val = fmt.Sprintf("%s  %s", WhoisAccent(m.Params[2]), WhoisValue(m.Trailing()))
		}
		field("actually", val)
	case "378": // RPL_WHOISHOST :is connecting from ...
		field("host", WhoisValue(m.Trailing()))
	case "379": // RPL_WHOISMODES :is using modes +iwx
		field("modes", WhoisAccent(strings.TrimPrefix(m.Trailing(), "is using modes ")))
	case "671", "275": // RPL_WHOISSECURE
		field("secure",
			fmt.Sprintf("%s%s  %s",
				theme.WhoisAccent, "TLS", theme.Reset)+WhoisValue(m.Trailing()))
	case "307": // is a registered nick (services)
		field("registered", WhoisValue(m.Trailing()))
	case "320": // is identified to services / extra info
		field("extra", WhoisValue(m.Trailing()))
	}
}

func (a *App) onPrivmsg(now time.Time, m *irc.Message, isNotice bool) {
	target := m.Target()
	text := m.Trailing()

	// Drop messages from ignored nicks entirely.
	if a.ignore.Has(m.Nick) {
		return
	}

	// Pull any embedded CTCP segments out first; the leftover is treated as
	// normal channel/query text (per CTCP spec a message can interleave both).
	plain, segs := ExtractCTCP(text)

	for _, seg := range segs {
		a.handleCTCP(now, m.Nick, target, seg.Kind, seg.Args, isNotice)
	}

	if plain == "" {
		return
	}

	// Find or create target buffer for the plain text portion.
	var b *Buffer
	if irc.IsChannel(target) {
		b = a.findBuffer(target)
		if b == nil {
			b = a.addBuffer(target, BufChannel)
		}
	} else {
		key := m.Nick
		if isNotice && m.Nick == "" {
			key = "(status)"
		}
		b = a.findBuffer(key)
		if b == nil && key != "(status)" {
			b = a.addBuffer(key, BufQuery)
		}
		if b == nil {
			b = a.statusBuf()
		}
	}

	mention := Mention(plain, a.curNick)
	if isNotice {
		a.writeRaw(b, FormatNotice(now, m.Nick, target, plain), ActHilite)
	} else {
		act := ActMsg
		if mention {
			act = ActHilite
		}
		if b.Kind == BufQuery {
			act = ActHilite
		}
		a.writeRaw(b, FormatPrivmsg(now, m.Nick, "", plain, false, mention), act)
	}
}

// handleCTCP dispatches a single parsed CTCP segment. For requests we also
// emit an auto-reply where appropriate (VERSION/PING/TIME/CLIENTINFO/etc.).
func (a *App) handleCTCP(now time.Time, from, target, kind, rest string, isNotice bool) {
	// ACTION is rendered as /me in the right buffer (works as a regular
	// channel/query message in both requests and replies, though replies
	// are nonsensical for ACTION).
	if kind == "ACTION" {
		b := a.findBuffer(target)
		if !irc.IsChannel(target) {
			b = a.findBuffer(from)
			if b == nil {
				b = a.addBuffer(from, BufQuery)
			}
		}
		if b == nil {
			b = a.statusBuf()
		}
		a.writeRaw(b, FormatAction(now, from, rest), ActMsg)
		return
	}

	if isNotice {
		// CTCP reply (came in via NOTICE) — show in the status buffer.
		if kind == "PING" {
			if ms, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64); err == nil {
				sent := time.UnixMilli(ms)
				if !sent.IsZero() && time.Since(sent) >= 0 && time.Since(sent) < 24*time.Hour {
					a.writeRaw(a.statusBuf(),
						FormatCTCPPingReply(now, from, time.Since(sent)), ActLow)
					return
				}
			}
		}
		a.writeRaw(a.statusBuf(), FormatCTCPReply(now, from, kind, rest), ActLow)
		return
	}

	// DCC is a request but uses a separate sub-protocol; route it to
	// the DCC handler and do not echo or auto-reply via CTCP.
	if kind == "DCC" {
		a.handleDCCOffer(now, from, rest)
		return
	}

	// Auto-reply where appropriate.
	switch kind {
	case "VERSION":
		a.ctcpReply(from, "VERSION", fmt.Sprintf("beacon %s :go irc :the rock burns clean", a.cfg.Version))
	case "PING":
		a.ctcpReply(from, "PING", rest)
	case "TIME":
		a.ctcpReply(from, "TIME", time.Now().Format(time.RFC1123))
	case "CLIENTINFO":
		a.ctcpReply(from, "CLIENTINFO",
			"ACTION CLIENTINFO FINGER PING SOURCE TIME USERINFO VERSION")
	case "USERINFO":
		a.ctcpReply(from, "USERINFO", a.cfg.Realname)
	case "SOURCE":
		a.ctcpReply(from, "SOURCE",
			"beacon — https://example.invalid/beacon (a BitchX-inspired irc client)")
	case "FINGER":
		a.ctcpReply(from, "FINGER",
			fmt.Sprintf("%s (beacon %s)", a.cfg.Realname, a.cfg.Version))
	}
	// Always show that we received it.
	a.writeRaw(a.statusBuf(), FormatCTCPRequest(now, from, kind, rest), ActLow)
}

// ctcpReply sends a CTCP reply (NOTICE-wrapped) to nick. Silently no-op if
// disconnected.
func (a *App) ctcpReply(to, kind, args string) {
	a.connMu.Lock()
	c := a.conn
	a.connMu.Unlock()
	if c == nil {
		return
	}
	payload := kind
	if args != "" {
		payload = kind + " " + args
	}
	_ = c.WriteRaw(fmt.Sprintf("NOTICE %s :\x01%s\x01", to, payload))
}

// ctcpRequest sends a CTCP request to target (PRIVMSG-wrapped) and echoes
// the outgoing CTCP into the active buffer.
func (a *App) ctcpRequest(target, kind, args string) error {
	a.connMu.Lock()
	c := a.conn
	a.connMu.Unlock()
	if c == nil {
		return fmt.Errorf("not connected")
	}
	payload := kind
	if args != "" {
		payload = kind + " " + args
	}
	if err := c.WriteRaw(fmt.Sprintf("PRIVMSG %s :\x01%s\x01", target, payload)); err != nil {
		return err
	}
	a.writeRaw(a.activeBuffer(), FormatCTCPSent(time.Now(), target, kind, args), ActLow)
	return nil
}

// ensure unused imports are kept honest
// saslPlainPayload builds the base64-encoded SASL PLAIN credential string:
// "\x00<user>\x00<pass>". The authzid (first field) is left empty so the
// server uses the authcid as the identity.
func saslPlainPayload(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
}

var _ = tls.VersionTLS12
var _ = strconv.Itoa
