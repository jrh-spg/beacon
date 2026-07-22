package dcc

import (
	"net"
	"testing"
)

func TestEncodeDecodeIP(t *testing.T) {
	cases := []string{"1.2.3.4", "192.168.0.1", "8.8.8.8", "255.255.255.255"}
	for _, c := range cases {
		ip := net.ParseIP(c)
		s, err := EncodeIP(ip)
		if err != nil {
			t.Fatalf("encode %s: %v", c, err)
		}
		got, err := decodeIP(s)
		if err != nil {
			t.Fatalf("decode %s: %v", s, err)
		}
		if got.To4().String() != c {
			t.Errorf("roundtrip %s -> %s -> %s", c, s, got)
		}
	}
}

func TestParseCTCPSend(t *testing.T) {
	// 16909060 == 1.2.3.4
	o, err := ParseCTCP("alice", "SEND hello.txt 16909060 6000 4096")
	if err != nil {
		t.Fatal(err)
	}
	if o.Kind != KindSend || o.Filename != "hello.txt" ||
		o.IP.String() != "1.2.3.4" || o.Port != 6000 || o.Size != 4096 {
		t.Errorf("parse mismatch: %+v", o)
	}
}

func TestParseCTCPChat(t *testing.T) {
	o, err := ParseCTCP("bob", "CHAT chat 16909060 7000")
	if err != nil {
		t.Fatal(err)
	}
	if o.Kind != KindChat || o.IP.String() != "1.2.3.4" || o.Port != 7000 {
		t.Errorf("parse mismatch: %+v", o)
	}
}

func TestParseCTCPMalformed(t *testing.T) {
	if _, err := ParseCTCP("x", "SEND"); err == nil {
		t.Error("expected error on truncated SEND")
	}
	if _, err := ParseCTCP("x", "WAT foo 1 2"); err == nil {
		t.Error("expected error on unknown subtype")
	}
}
