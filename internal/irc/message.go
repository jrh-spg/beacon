// Package irc implements a minimal RFC 1459/2812 client-side protocol layer.
package irc

import (
	"errors"
	"strings"
)

// Message is a parsed IRC protocol line.
type Message struct {
	Raw     string
	Prefix  string // optional, no leading ':'
	Nick    string // parsed from prefix if it looks like nick!user@host
	User    string
	Host    string
	Command string
	Params  []string // includes any trailing (last) parameter
}

// ParseMessage parses a single IRC line (without CRLF).
func ParseMessage(line string) (*Message, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("empty line")
	}

	m := &Message{Raw: line}
	pos := 0

	// optional tags (IRCv3) - skip them
	if line[0] == '@' {
		sp := strings.IndexByte(line, ' ')
		if sp == -1 {
			return nil, errors.New("malformed tags")
		}
		pos = sp + 1
		for pos < len(line) && line[pos] == ' ' {
			pos++
		}
	}

	// optional prefix
	if pos < len(line) && line[pos] == ':' {
		sp := strings.IndexByte(line[pos:], ' ')
		if sp == -1 {
			return nil, errors.New("malformed prefix")
		}
		m.Prefix = line[pos+1 : pos+sp]
		pos += sp + 1
		for pos < len(line) && line[pos] == ' ' {
			pos++
		}

		// nick!user@host parse
		if i := strings.IndexByte(m.Prefix, '!'); i != -1 {
			m.Nick = m.Prefix[:i]
			rest := m.Prefix[i+1:]
			if j := strings.IndexByte(rest, '@'); j != -1 {
				m.User = rest[:j]
				m.Host = rest[j+1:]
			} else {
				m.User = rest
			}
		} else if !strings.ContainsAny(m.Prefix, ".") {
			m.Nick = m.Prefix
		}
	}

	// command
	if pos >= len(line) {
		return nil, errors.New("missing command")
	}
	sp := strings.IndexByte(line[pos:], ' ')
	if sp == -1 {
		m.Command = strings.ToUpper(line[pos:])
		return m, nil
	}
	m.Command = strings.ToUpper(line[pos : pos+sp])
	pos += sp + 1
	for pos < len(line) && line[pos] == ' ' {
		pos++
	}

	// params
	for pos < len(line) {
		if line[pos] == ':' {
			m.Params = append(m.Params, line[pos+1:])
			break
		}
		sp := strings.IndexByte(line[pos:], ' ')
		if sp == -1 {
			m.Params = append(m.Params, line[pos:])
			break
		}
		m.Params = append(m.Params, line[pos:pos+sp])
		pos += sp + 1
		for pos < len(line) && line[pos] == ' ' {
			pos++
		}
	}

	return m, nil
}

// Trailing returns the last parameter or empty string.
func (m *Message) Trailing() string {
	if len(m.Params) == 0 {
		return ""
	}
	return m.Params[len(m.Params)-1]
}

// Target returns the first parameter (usually channel or target nick).
func (m *Message) Target() string {
	if len(m.Params) == 0 {
		return ""
	}
	return m.Params[0]
}

// IsChannel reports whether a target name is a channel (starts with # & + !).
func IsChannel(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '#', '&', '+', '!':
		return true
	}
	return false
}
