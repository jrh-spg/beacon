package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadStartupOptionsFromYAML(t *testing.T) {
	path := writeConfig(t, `server: irc.example.org
port: 6697
tls: false
tls-insecure: true
cert: client.pem
key: client.key
sasl: plain
sasl-user: account
sasl-pass: secret
nick: cfgNick
altnick: cfgNick_
user: ident
realname: Config User
password: server-pass
join:
  - '#go'
  - '#linux'
`)

	opts, err := loadStartupOptions(path, true, nil, startupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Server != "irc.example.org:6697" {
		t.Fatalf("Server = %q", opts.Server)
	}
	if !opts.UseTLS {
		t.Fatal("UseTLS = false, want true because port 6697 implies TLS")
	}
	if !opts.TLSSkipVerify {
		t.Fatal("TLSSkipVerify = false, want true")
	}
	if opts.SASLMechanism != "PLAIN" || opts.SASLUser != "account" || opts.SASLPass != "secret" {
		t.Fatalf("SASL options = %#v", opts)
	}
	if opts.CertFile != "client.pem" || opts.KeyFile != "client.key" {
		t.Fatalf("cert/key = %q/%q", opts.CertFile, opts.KeyFile)
	}
	if opts.Nick != "cfgNick" || opts.AltNick != "cfgNick_" || opts.User != "ident" || opts.Realname != "Config User" {
		t.Fatalf("identity options = %#v", opts)
	}
	if opts.Password != "server-pass" {
		t.Fatalf("Password = %q", opts.Password)
	}
	if !reflect.DeepEqual(opts.AutoJoin, []string{"#go", "#linux"}) {
		t.Fatalf("AutoJoin = %#v", opts.AutoJoin)
	}
}

func TestLoadStartupOptionsCLIOverridesYAML(t *testing.T) {
	path := writeConfig(t, `server: irc.example.org
port: 6667
tls: true
nick: cfgNick
join: '#cfg,#old'
`)
	cli := startupOptions{
		Server:   "irc.libera.chat",
		UseTLS:   false,
		Nick:     "cliNick",
		AutoJoin: []string{"#cli"},
	}
	explicit := map[string]bool{
		"server": true,
		"tls":    true,
		"nick":   true,
		"join":   true,
	}

	opts, err := loadStartupOptions(path, true, explicit, cli)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Server != "irc.libera.chat:6667" {
		t.Fatalf("Server = %q", opts.Server)
	}
	if opts.UseTLS {
		t.Fatal("UseTLS = true, want CLI false override")
	}
	if opts.Nick != "cliNick" {
		t.Fatalf("Nick = %q", opts.Nick)
	}
	if !reflect.DeepEqual(opts.AutoJoin, []string{"#cli"}) {
		t.Fatalf("AutoJoin = %#v", opts.AutoJoin)
	}
}

func TestReadYAMLConfigUnknownField(t *testing.T) {
	path := writeConfig(t, "server: irc.example.org\nunknown: true\n")
	_, _, err := readYAMLConfig(path, true)
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("readYAMLConfig error = %v", err)
	}
}

func TestReadYAMLConfigMissingOptionalFile(t *testing.T) {
	_, loaded, err := readYAMLConfig(filepath.Join(t.TempDir(), "missing.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("loaded = true, want false")
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" #one, ,#two,#three ")
	want := []string{"#one", "#two", "#three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitList = %#v, want %#v", got, want)
	}
}
