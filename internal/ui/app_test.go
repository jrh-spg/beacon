package ui

import (
	"strings"
	"testing"

	"beacon/internal/irc"
)

func TestDispatchModeChannelGoesToChannelBuffer(t *testing.T) {
	a := New(Config{Nick: "me", User: "me", Realname: "me"})
	a.curNick = "me"
	a.dispatch(&irc.Message{
		Prefix: "me!me@host", Nick: "me",
		Command: "JOIN", Params: []string{"#test"},
	})
	a.dispatch(&irc.Message{
		Prefix: "op!op@host", Nick: "op",
		Command: "MODE", Params: []string{"#test", "+o", "victim"},
	})

	if strings.Contains(a.statusBuf().View.GetText(true), "sets mode") {
		t.Fatal("channel MODE change leaked into the status buffer")
	}
	chanBuf := a.findBuffer("#test")
	if !strings.Contains(chanBuf.View.GetText(true), "sets mode +o victim on #test") {
		t.Fatalf("channel MODE change missing from channel buffer: %q", chanBuf.View.GetText(true))
	}
}

func TestDispatchModeSelfGoesToActiveBuffer(t *testing.T) {
	a := New(Config{Nick: "me", User: "me", Realname: "me"})
	a.curNick = "me"
	a.dispatch(&irc.Message{
		Prefix: "me!me@host", Nick: "me",
		Command: "JOIN", Params: []string{"#test"},
	})
	a.dispatch(&irc.Message{
		Prefix: "me!me@host", Nick: "me",
		Command: "MODE", Params: []string{"me", "+i"},
	})

	chanBuf := a.findBuffer("#test")
	if !strings.Contains(chanBuf.View.GetText(true), "sets mode +i on me") {
		t.Fatalf("self MODE change missing from active buffer: %q", chanBuf.View.GetText(true))
	}
	if strings.Contains(a.statusBuf().View.GetText(true), "sets mode") {
		t.Fatal("self MODE change unexpectedly landed in the status buffer")
	}
}
