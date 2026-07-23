package ui

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"beacon/internal/dcc"
	"beacon/internal/theme"
)

// dccTransfer is one bookkeeping entry for a transfer or chat — sufficient
// to display in /dcc list and to cancel via /dcc close.
type dccTransfer struct {
	id     int
	kind   dcc.Kind
	peer   string
	file   string
	size   int64
	got    int64 // bytes transferred so far
	state  string
	cancel func()
}

// dccState aggregates pending offers and live transfers/chats.
type dccState struct {
	mu        sync.Mutex
	offers    map[string]*dcc.Offer // pending inbound offers keyed by peer nick (lowercased)
	transfers map[int]*dccTransfer
	chats     map[string]net.Conn // active CHAT sockets keyed by peer nick (lowercased)
	nextID    int
}

func newDCCState() *dccState {
	return &dccState{
		offers:    map[string]*dcc.Offer{},
		transfers: map[int]*dccTransfer{},
		chats:     map[string]net.Conn{},
	}
}

// ----------------------------------------------------------------------------
// Format helpers
// ----------------------------------------------------------------------------

// FormatDCCOffer renders an inbound DCC offer announcement.
func FormatDCCOffer(t time.Time, o *dcc.Offer) string {
	switch o.Kind {
	case dcc.KindSend:
		return line(t, fmt.Sprintf(
			"%s[%sDCC%s]%s offer from %s%s%s : %sSEND%s %s%s%s (%s%s%s) — %s/dcc accept %s%s",
			theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
			theme.NickOther, tview.Escape(o.From), theme.Reset,
			theme.WhoisAccent, theme.Reset,
			theme.Channel, tview.Escape(o.Filename), theme.Reset,
			theme.Info, dcc.HumanSize(o.Size), theme.Reset,
			theme.Action, tview.Escape(o.From), theme.Reset,
		))
	case dcc.KindChat:
		return line(t, fmt.Sprintf(
			"%s[%sDCC%s]%s offer from %s%s%s : %sCHAT%s — %s/dcc accept %s%s",
			theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
			theme.NickOther, tview.Escape(o.From), theme.Reset,
			theme.WhoisAccent, theme.Reset,
			theme.Action, tview.Escape(o.From), theme.Reset,
		))
	}
	return line(t, "DCC: unknown offer")
}

// FormatDCCInfo renders a generic DCC info/progress/event line.
func FormatDCCInfo(t time.Time, text string) string {
	return line(t, fmt.Sprintf("%s[%sDCC%s]%s %s%s%s",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.Text, tview.Escape(text), theme.Reset))
}

// FormatDCCError renders a DCC failure.
func FormatDCCError(t time.Time, text string) string {
	return line(t, fmt.Sprintf("%s[%sDCC%s]%s %s!!%s %s",
		theme.Bracket, theme.CTCP, theme.Bracket, theme.Reset,
		theme.Error, theme.Reset, tview.Escape(text)))
}

// ----------------------------------------------------------------------------
// Public entrypoint from CTCP dispatch
// ----------------------------------------------------------------------------

// handleDCCOffer is called when an inbound CTCP DCC message arrives. It
// parses, records, and announces the offer; if dcc_auto_accept is set it
// also kicks off the accept path.
func (a *App) handleDCCOffer(now time.Time, from, body string) {
	o, err := dcc.ParseCTCP(from, body)
	if err != nil {
		a.writeRaw(a.statusBuf(), FormatDCCError(now, "parse: "+err.Error()), ActMsg)
		return
	}
	a.dcc.mu.Lock()
	a.dcc.offers[strings.ToLower(from)] = o
	a.dcc.mu.Unlock()
	a.writeRaw(a.statusBuf(), FormatDCCOffer(now, o), ActHilite)
	if a.settings.Bool("dcc_auto_accept") {
		a.acceptOffer(o, "")
	}
}

// ----------------------------------------------------------------------------
// /dcc command
// ----------------------------------------------------------------------------

func (a *App) cmdDCC(args string) {
	args = strings.TrimSpace(args)
	if args == "" {
		a.dccHelp()
		return
	}
	parts := strings.SplitN(args, " ", 2)
	sub := strings.ToLower(parts[0])
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}
	switch sub {
	case "list", "ls":
		a.dccList()
	case "accept", "get":
		a.dccAccept(rest)
	case "send":
		a.dccSend(rest)
	case "chat":
		a.dccChat(rest)
	case "close", "cancel":
		a.dccClose(rest)
	default:
		a.dccHelp()
	}
}

func (a *App) dccHelp() {
	for _, l := range []string{
		"/dcc list                       show pending offers + active transfers",
		"/dcc accept <nick> [save-path]  accept the most recent offer from <nick>",
		"/dcc send <nick> <file>         offer <file> to <nick>",
		"/dcc chat <nick>                offer a DCC chat session to <nick>",
		"/dcc close <nick|id>            cancel an offer / abort a transfer",
	} {
		a.printlnInfo(l)
	}
}

func (a *App) dccList() {
	a.dcc.mu.Lock()
	defer a.dcc.mu.Unlock()
	if len(a.dcc.offers) == 0 && len(a.dcc.transfers) == 0 {
		a.printlnInfo("dcc: nothing pending")
		return
	}
	for nick, o := range a.dcc.offers {
		desc := "CHAT"
		if o.Kind == dcc.KindSend {
			desc = fmt.Sprintf("SEND %s (%s)", o.Filename, dcc.HumanSize(o.Size))
		}
		a.printlnInfo(fmt.Sprintf("dcc offer  %s  %s  %s:%d", nick, desc, o.IP, o.Port))
	}
	for _, x := range a.dcc.transfers {
		a.printlnInfo(fmt.Sprintf("dcc xfer   #%d  %s  %s  %s  %s/%s",
			x.id, x.kind, x.peer, x.state,
			dcc.HumanSize(x.got), dcc.HumanSize(x.size)))
	}
}

// dccAccept handles /dcc accept <nick> [save-path]. The save path is only
// relevant for KindSend; for KindChat it is ignored.
func (a *App) dccAccept(rest string) {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	if parts[0] == "" {
		a.printlnError("usage: /dcc accept <nick> [save-path]")
		return
	}
	nick := parts[0]
	savePath := ""
	if len(parts) > 1 {
		savePath = parts[1]
	}
	a.dcc.mu.Lock()
	o := a.dcc.offers[strings.ToLower(nick)]
	if o != nil {
		delete(a.dcc.offers, strings.ToLower(nick))
	}
	a.dcc.mu.Unlock()
	if o == nil {
		a.printlnError("dcc: no pending offer from " + nick)
		return
	}
	a.acceptOffer(o, savePath)
}

func (a *App) acceptOffer(o *dcc.Offer, savePath string) {
	switch o.Kind {
	case dcc.KindSend:
		dest := savePath
		if dest == "" {
			dest = filepath.Join(a.settings.Get("dcc_download_dir"), filepath.Base(o.Filename))
		}
		a.startReceive(o, dest)
	case dcc.KindChat:
		a.startChatClient(o)
	}
}

func (a *App) startReceive(o *dcc.Offer, dest string) {
	id := a.dccRegister(dcc.KindSend, o.From, dest, o.Size, "receiving", func() {})
	a.printlnInfo(fmt.Sprintf("dcc: receiving %s from %s -> %s", o.Filename, o.From, dest))

	go func() {
		err := dcc.Receive(o.IP, o.Port, dest, o.Size,
			func(got, total int64) { a.dccProgress(id, got) })
		a.dccFinish(id, err)
		if err != nil {
			a.writeRaw(a.statusBuf(), FormatDCCError(time.Now(),
				fmt.Sprintf("receive %s from %s: %v", o.Filename, o.From, err)), ActMsg)
			return
		}
		a.writeRaw(a.statusBuf(), FormatDCCInfo(time.Now(),
			fmt.Sprintf("received %s from %s (%s) -> %s",
				o.Filename, o.From, dcc.HumanSize(o.Size), dest)), ActMsg)
	}()
}

// dccSend offers a file to a nick by listening on an ephemeral TCP port
// and sending the appropriate CTCP message via the IRC connection.
func (a *App) dccSend(rest string) {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		a.printlnError("usage: /dcc send <nick> <file>")
		return
	}
	nick := parts[0]
	path := expandUser(parts[1])
	if !a.requireConn() {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		a.printlnError("dcc send: " + err.Error())
		return
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		a.printlnError("dcc send: " + err.Error())
		return
	}

	l, err := dcc.Listen()
	if err != nil {
		f.Close()
		a.printlnError("dcc send: " + err.Error())
		return
	}
	ipStr, err := dcc.EncodeIP(a.localIPv4())
	if err != nil {
		l.Close()
		f.Close()
		a.printlnError("dcc send: " + err.Error())
		return
	}

	id := a.dccRegister(dcc.KindSend, nick, path, st.Size(), "offered",
		func() { l.Close(); f.Close() })

	// Announce.
	_ = a.conn.WriteRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC SEND %s %s %d %d\x01",
		nick, filepath.Base(path), ipStr, l.Port, st.Size()))
	a.printlnInfo(fmt.Sprintf("dcc: offering %s (%s) to %s on port %d",
		path, dcc.HumanSize(st.Size()), nick, l.Port))

	go func() {
		c, err := l.Accept(120 * time.Second)
		if err != nil {
			a.writeRaw(a.statusBuf(), FormatDCCError(time.Now(),
				fmt.Sprintf("send %s to %s: %v", path, nick, err)), ActMsg)
			a.dccFinish(id, err)
			f.Close()
			return
		}
		a.dccSetState(id, "sending")
		err = dcc.SendFile(c, f, st.Size(),
			func(sent, total int64) { a.dccProgress(id, sent) })
		a.dccFinish(id, err)
		if err != nil {
			a.writeRaw(a.statusBuf(), FormatDCCError(time.Now(),
				fmt.Sprintf("send %s to %s: %v", path, nick, err)), ActMsg)
			return
		}
		a.writeRaw(a.statusBuf(), FormatDCCInfo(time.Now(),
			fmt.Sprintf("sent %s (%s) to %s", path, dcc.HumanSize(st.Size()), nick)), ActMsg)
	}()
}

// dccChat offers a chat session to a nick.
func (a *App) dccChat(rest string) {
	nick := strings.TrimSpace(rest)
	if nick == "" {
		a.printlnError("usage: /dcc chat <nick>")
		return
	}
	if !a.requireConn() {
		return
	}
	l, err := dcc.Listen()
	if err != nil {
		a.printlnError("dcc chat: " + err.Error())
		return
	}
	ipStr, err := dcc.EncodeIP(a.localIPv4())
	if err != nil {
		l.Close()
		a.printlnError("dcc chat: " + err.Error())
		return
	}
	id := a.dccRegister(dcc.KindChat, nick, "", 0, "offered", func() { l.Close() })

	_ = a.conn.WriteRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC CHAT chat %s %d\x01",
		nick, ipStr, l.Port))
	a.printlnInfo(fmt.Sprintf("dcc: chat offered to %s on port %d", nick, l.Port))

	go func() {
		c, err := l.Accept(120 * time.Second)
		if err != nil {
			a.writeRaw(a.statusBuf(), FormatDCCError(time.Now(),
				fmt.Sprintf("chat to %s: %v", nick, err)), ActMsg)
			a.dccFinish(id, err)
			return
		}
		a.dccSetState(id, "chatting")
		a.runChat(nick, c)
		a.dccFinish(id, nil)
	}()
}

func (a *App) startChatClient(o *dcc.Offer) {
	id := a.dccRegister(dcc.KindChat, o.From, "", 0, "connecting", func() {})
	go func() {
		c, err := dcc.DialChat(o.IP, o.Port)
		if err != nil {
			a.dccFinish(id, err)
			a.writeRaw(a.statusBuf(), FormatDCCError(time.Now(),
				fmt.Sprintf("chat with %s: %v", o.From, err)), ActMsg)
			return
		}
		a.dccSetState(id, "chatting")
		a.runChat(o.From, c)
		a.dccFinish(id, nil)
	}()
}

// runChat owns a connected DCC chat socket. It opens a buffer (kind BufDCC)
// named "=nick", reads inbound lines onto it, and stores the conn so
// outbound input can write to it.
func (a *App) runChat(peer string, c net.Conn) {
	bufName := "=" + peer
	b := a.addBuffer(bufName, BufDCC)
	a.switchToName(bufName)

	a.dcc.mu.Lock()
	a.dcc.chats[strings.ToLower(peer)] = c
	a.dcc.mu.Unlock()

	a.writeRaw(b, FormatDCCInfo(time.Now(),
		fmt.Sprintf("dcc chat established with %s (%s)", peer, c.RemoteAddr())), ActLow)

	defer func() {
		c.Close()
		a.dcc.mu.Lock()
		delete(a.dcc.chats, strings.ToLower(peer))
		a.dcc.mu.Unlock()
		a.writeRaw(b, FormatDCCInfo(time.Now(), "dcc chat closed"), ActLow)
	}()

	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			a.writeRaw(b, FormatPrivmsg(time.Now(), peer, "", line, false, false), ActMsg)
		}
		if err != nil {
			return
		}
	}
}

// dccSendChat is called by the input dispatcher when the user types in a
// DCC chat buffer.
func (a *App) dccSendChat(peer, text string) bool {
	text = expandEmojiCodes(text)
	a.dcc.mu.Lock()
	c := a.dcc.chats[strings.ToLower(peer)]
	a.dcc.mu.Unlock()
	if c == nil {
		return false
	}
	if _, err := fmt.Fprintf(c, "%s\r\n", text); err != nil {
		a.printlnError("dcc chat: " + err.Error())
		return true
	}
	a.writeRaw(a.activeBuffer(),
		FormatPrivmsg(time.Now(), a.curNick, "", text, true, false), ActLow)
	return true
}

// dccClose tears down a transfer or pending offer.
func (a *App) dccClose(rest string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		a.printlnError("usage: /dcc close <nick|id>")
		return
	}
	// Try numeric id first.
	if id := atoiSafe(rest); id > 0 {
		a.dcc.mu.Lock()
		x := a.dcc.transfers[id]
		a.dcc.mu.Unlock()
		if x != nil && x.cancel != nil {
			x.cancel()
			a.dccFinish(id, fmt.Errorf("cancelled"))
			a.printlnInfo(fmt.Sprintf("dcc: cancelled #%d", id))
			return
		}
	}
	// Otherwise treat as nick.
	a.dcc.mu.Lock()
	delete(a.dcc.offers, strings.ToLower(rest))
	c := a.dcc.chats[strings.ToLower(rest)]
	a.dcc.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
	a.printlnInfo("dcc: cleared pending state for " + rest)
}

// ----------------------------------------------------------------------------
// Bookkeeping helpers
// ----------------------------------------------------------------------------

func (a *App) dccRegister(k dcc.Kind, peer, file string, size int64, state string, cancel func()) int {
	a.dcc.mu.Lock()
	defer a.dcc.mu.Unlock()
	a.dcc.nextID++
	id := a.dcc.nextID
	a.dcc.transfers[id] = &dccTransfer{
		id: id, kind: k, peer: peer, file: file, size: size,
		state: state, cancel: cancel,
	}
	return id
}

func (a *App) dccSetState(id int, state string) {
	a.dcc.mu.Lock()
	if x := a.dcc.transfers[id]; x != nil {
		x.state = state
	}
	a.dcc.mu.Unlock()
}

func (a *App) dccProgress(id int, got int64) {
	a.dcc.mu.Lock()
	if x := a.dcc.transfers[id]; x != nil {
		x.got = got
	}
	a.dcc.mu.Unlock()
}

func (a *App) dccFinish(id int, err error) {
	a.dcc.mu.Lock()
	if x := a.dcc.transfers[id]; x != nil {
		if err == nil {
			x.state = "done"
		} else {
			x.state = "failed"
		}
	}
	a.dcc.mu.Unlock()
}

// localIPv4 returns the local IPv4 address of the active IRC connection,
// or a sensible fallback so we never emit 0.0.0.0 to a peer.
func (a *App) localIPv4() net.IP {
	a.connMu.Lock()
	c := a.conn
	a.connMu.Unlock()
	if c != nil {
		// Conn.RemoteAddr is "host:port"; we need our LocalAddr instead,
		// which isn't exposed. Fall back to a UDP-probe trick that
		// determines which interface would route to a public address.
	}
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err == nil {
		defer conn.Close()
		if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return a.IP.To4()
		}
	}
	return net.IPv4(127, 0, 0, 1).To4()
}

func expandUser(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
