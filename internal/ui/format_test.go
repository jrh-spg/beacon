package ui

import "testing"

func TestParseEmojiDB(t *testing.T) {
	db := parseEmojiDB(":smile:\n🙂\nmalformed\n:smile:\n😄\n:blank:\n\n")
	if got := db[":smile:"]; got != "😄" {
		t.Fatalf("parseEmojiDB duplicate = %q, want 😄", got)
	}
	if _, ok := db[":blank:"]; ok {
		t.Fatal("parseEmojiDB kept blank emoji entry")
	}
}

func TestExpandEmojiCodes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "coffee :coffee:", want: "coffee ☕"},
		{in: ":poop::coffee:", want: "💩☕"},
		{in: "unknown :nope:", want: "unknown :nope:"},
		{in: "unterminated :coffee", want: "unterminated :coffee"},
	}
	for _, c := range cases {
		if got := expandEmojiCodes(c.in); got != c.want {
			t.Errorf("expandEmojiCodes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIRCFormatExpandsEmojiCodes(t *testing.T) {
	got := IRCFormat("hi :coffee: [ok]")
	if got != "hi ☕ [[]ok]" {
		t.Fatalf("IRCFormat emoji/bracket = %q", got)
	}
}

func TestCompleteEmoji(t *testing.T) {
	matches := completeEmoji(":poop")
	if len(matches) != 1 || matches[0] != "💩" {
		t.Fatalf("completeEmoji(:poop) = %#v, want [💩]", matches)
	}
	if matches := completeEmoji("poop"); len(matches) != 0 {
		t.Fatalf("completeEmoji without colon = %#v, want none", matches)
	}
}

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
