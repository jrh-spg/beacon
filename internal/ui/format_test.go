package ui

import "testing"

func TestExtractCTCP(t *testing.T) {
	cases := []struct {
		in    string
		text  string
		count int
		first string
		args  string
	}{
		{
			in:    "\x01VERSION\x01",
			count: 1,
			first: "VERSION",
		},
		{
			in:    "\x01PING 1234567890\x01",
			count: 1,
			first: "PING",
			args:  "1234567890",
		},
		{
			in:    "hello \x01ACTION waves\x01 world",
			text:  "hello  world",
			count: 1,
			first: "ACTION",
			args:  "waves",
		},
		{
			in:    "plain text only",
			text:  "plain text only",
			count: 0,
		},
		{
			in:    "\x01ONE\x01 between \x01TWO arg\x01",
			text:  "between",
			count: 2,
			first: "ONE",
		},
		{
			// unterminated trailing \x01 — must not panic
			in:   "trailing \x01OOPS no close",
			text: "trailing \x01OOPS no close",
		},
	}

	for _, c := range cases {
		text, segs := ExtractCTCP(c.in)
		if c.text != "" && text != c.text {
			t.Errorf("ExtractCTCP(%q) text = %q, want %q", c.in, text, c.text)
		}
		if len(segs) != c.count {
			t.Errorf("ExtractCTCP(%q) segs = %d, want %d", c.in, len(segs), c.count)
		}
		if c.count > 0 && c.first != "" && segs[0].Kind != c.first {
			t.Errorf("ExtractCTCP(%q) first kind = %q, want %q", c.in, segs[0].Kind, c.first)
		}
		if c.count > 0 && c.args != "" && segs[0].Args != c.args {
			t.Errorf("ExtractCTCP(%q) first args = %q, want %q", c.in, segs[0].Args, c.args)
		}
	}
}
