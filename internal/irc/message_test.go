package irc

import "testing"

func TestParseMessage(t *testing.T) {
	cases := []struct {
		in       string
		prefix   string
		nick     string
		user     string
		host     string
		command  string
		params   []string
		trailing string
	}{
		{
			in:       "PING :tolsun.oulu.fi",
			command:  "PING",
			params:   []string{"tolsun.oulu.fi"},
			trailing: "tolsun.oulu.fi",
		},
		{
			in:       ":Angel!wings@irc.org PRIVMSG Wiz :Are you receiving this message?",
			prefix:   "Angel!wings@irc.org",
			nick:     "Angel",
			user:     "wings",
			host:     "irc.org",
			command:  "PRIVMSG",
			params:   []string{"Wiz", "Are you receiving this message?"},
			trailing: "Are you receiving this message?",
		},
		{
			in:      ":irc.example.net 353 me = #chan :@op +voice plain",
			prefix:  "irc.example.net",
			command: "353",
			params:  []string{"me", "=", "#chan", "@op +voice plain"},
		},
		{
			in:      ":me!~me@host JOIN #chan",
			prefix:  "me!~me@host",
			nick:    "me",
			user:    "~me",
			host:    "host",
			command: "JOIN",
			params:  []string{"#chan"},
		},
		{
			in:      "@time=123 :nick!u@h NOTICE #chan :hi",
			prefix:  "nick!u@h",
			nick:    "nick",
			user:    "u",
			host:    "h",
			command: "NOTICE",
			params:  []string{"#chan", "hi"},
		},
	}

	for _, c := range cases {
		m, err := ParseMessage(c.in)
		if err != nil {
			t.Fatalf("parse %q: %v", c.in, err)
		}
		if m.Command != c.command {
			t.Errorf("%q: command %q != %q", c.in, m.Command, c.command)
		}
		if c.prefix != "" && m.Prefix != c.prefix {
			t.Errorf("%q: prefix %q != %q", c.in, m.Prefix, c.prefix)
		}
		if c.nick != "" && m.Nick != c.nick {
			t.Errorf("%q: nick %q != %q", c.in, m.Nick, c.nick)
		}
		if c.user != "" && m.User != c.user {
			t.Errorf("%q: user %q != %q", c.in, m.User, c.user)
		}
		if c.host != "" && m.Host != c.host {
			t.Errorf("%q: host %q != %q", c.in, m.Host, c.host)
		}
		if len(m.Params) != len(c.params) {
			t.Errorf("%q: params %v != %v", c.in, m.Params, c.params)
			continue
		}
		for i := range c.params {
			if m.Params[i] != c.params[i] {
				t.Errorf("%q: param[%d] %q != %q", c.in, i, m.Params[i], c.params[i])
			}
		}
		if c.trailing != "" && m.Trailing() != c.trailing {
			t.Errorf("%q: trailing %q != %q", c.in, m.Trailing(), c.trailing)
		}
	}
}

func TestIsChannel(t *testing.T) {
	for _, s := range []string{"#go", "&local", "+x", "!ABCDE"} {
		if !IsChannel(s) {
			t.Errorf("IsChannel(%q) should be true", s)
		}
	}
	for _, s := range []string{"", "nick", "@op", "."} {
		if IsChannel(s) {
			t.Errorf("IsChannel(%q) should be false", s)
		}
	}
}
