// Command beacon is a terminal IRC client in the spirit of BitchX and irssi,
// with a BitchX-inspired theme and built-in TLS support.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"beacon/internal/ui"
)

const version = "0.1.1"

func main() {
	var (
		cli        startupOptions
		joinFlag   string
		configPath = defaultConfigPath()
		showVer    bool
	)
	flag.StringVar(&configPath, "config", configPath, "YAML config file (default: ~/.config/beacon/config.yaml if present)")
	flag.StringVar(&cli.Server, "server", "", "IRC server host[:port] to connect to on startup")
	flag.IntVar(&cli.Port, "port", 0, "port override (defaults to 6667, or 6697 with -tls)")
	flag.BoolVar(&cli.UseTLS, "tls", false, "use TLS for the initial server (also implied by -port 6697)")
	flag.BoolVar(&cli.TLSSkipVerify, "tls-insecure", false, "skip TLS certificate verification (use with care)")
	flag.StringVar(&cli.CertFile, "cert", "", "PEM client certificate for SASL EXTERNAL / NickServ CertFP")
	flag.StringVar(&cli.KeyFile, "key", "", "PEM private key for -cert (defaults to -cert file if combined PEM)")
	flag.StringVar(&cli.SASLMechanism, "sasl", "", "SASL mechanism: EXTERNAL (cert) or PLAIN (password)")
	flag.StringVar(&cli.SASLUser, "sasl-user", "", "SASL PLAIN username")
	flag.StringVar(&cli.SASLPass, "sasl-pass", "", "SASL PLAIN password")
	flag.StringVar(&cli.Nick, "nick", "", "nickname (default: $USER or 'beacon')")
	flag.StringVar(&cli.AltNick, "altnick", "", "alternate nickname if primary is in use")
	flag.StringVar(&cli.User, "user", "", "ident username (default: nick)")
	flag.StringVar(&cli.Realname, "realname", "", "GECOS / real name")
	flag.StringVar(&cli.Password, "password", "", "server password (PASS)")
	flag.StringVar(&joinFlag, "join", "", "comma-separated channels to auto-join on connect")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "beacon %s — a BitchX-inspired IRC client\n\n", version)
		fmt.Fprintf(os.Stderr, "usage: %s [flags]\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
examples:
  beacon -server irc.libera.chat -tls -nick myhandle -join '#go-nuts,#linux'
  beacon -server irc.example.org -port 6667 -nick rocknrolla
  beacon -config ~/.config/beacon/config.yaml
  beacon                   # launch with no connection; /server later`)
	}
	flag.Parse()

	if showVer {
		fmt.Printf("beacon %s\n", version)
		return
	}

	cli.AutoJoin = splitList(joinFlag)
	explicit := explicitFlags(flag.CommandLine)
	opts, err := loadStartupOptions(configPath, explicit["config"], explicit, cli)
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
		os.Exit(1)
	}

	cfg := ui.Config{
		Server:        opts.Server,
		UseTLS:        opts.UseTLS,
		TLSSkipVerify: opts.TLSSkipVerify,
		Nick:          opts.Nick,
		AltNick:       opts.AltNick,
		User:          opts.User,
		Realname:      opts.Realname,
		Password:      opts.Password,
		AutoJoin:      opts.AutoJoin,
		AutoConnect:   opts.Server != "",
		Version:       version,
		SASLMechanism: opts.SASLMechanism,
		SASLUser:      opts.SASLUser,
		SASLPass:      opts.SASLPass,
		CertFile:      opts.CertFile,
		KeyFile:       opts.KeyFile,
	}

	app := ui.New(cfg)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
		os.Exit(1)
	}
}

// sanitizeNick keeps only IRC-safe nick chars.
func sanitizeNick(s string) string {
	var b strings.Builder
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			r == '_' || r == '-' || r == '\\' || r == '[' || r == ']' ||
			r == '{' || r == '}' || r == '|' || r == '^' || r == '`'
		if !ok && i > 0 {
			ok = r >= '0' && r <= '9'
		}
		if ok {
			b.WriteRune(r)
		}
	}
	return b.String()
}
