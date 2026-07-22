// Command beacon is a terminal IRC client in the spirit of BitchX and irssi,
// with a BitchX-inspired theme and built-in TLS support.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"strings"

	"beacon/internal/ui"
)

const version = "0.1.0"

func main() {
	var (
		server    = flag.String("server", "", "IRC server host[:port] to connect to on startup")
		useTLS    = flag.Bool("tls", false, "use TLS for the initial server (also implied by -port 6697)")
		insecure  = flag.Bool("tls-insecure", false, "skip TLS certificate verification (use with care)")
		certFile  = flag.String("cert", "", "PEM client certificate for SASL EXTERNAL / NickServ CertFP")
		keyFile   = flag.String("key", "", "PEM private key for -cert (defaults to -cert file if combined PEM)")
		sasl      = flag.String("sasl", "", "SASL mechanism: EXTERNAL (cert) or PLAIN (password)")
		saslUser  = flag.String("sasl-user", "", "SASL PLAIN username")
		saslPass  = flag.String("sasl-pass", "", "SASL PLAIN password")
		nick      = flag.String("nick", "", "nickname (default: $USER or 'beacon')")
		altNick   = flag.String("altnick", "", "alternate nickname if primary is in use")
		userName  = flag.String("user", "", "ident username (default: nick)")
		realName  = flag.String("realname", "beacon user", "GECOS / real name")
		password  = flag.String("password", "", "server password (PASS)")
		joinFlag  = flag.String("join", "", "comma-separated channels to auto-join on connect")
		port      = flag.Int("port", 0, "port override (defaults to 6667, or 6697 with -tls)")
		showVer   = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "beacon %s — a BitchX-inspired IRC client\n\n", version)
		fmt.Fprintf(os.Stderr, "usage: %s [flags]\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
examples:
  beacon -server irc.libera.chat -tls -nick myhandle -join '#go-nuts,#linux'
  beacon -server irc.example.org -port 6667 -nick rocknrolla
  beacon                   # launch with no connection; /server later`)
	}
	flag.Parse()

	if *showVer {
		fmt.Printf("beacon %s\n", version)
		return
	}

	n := *nick
	if n == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			n = sanitizeNick(u.Username)
		}
	}
	if n == "" {
		n = "beacon"
	}
	un := *userName
	if un == "" {
		un = n
	}
	alt := *altNick
	if alt == "" {
		alt = n + "_"
	}

	srv := *server
	if srv != "" && *port != 0 {
		// strip any existing :port
		if i := strings.LastIndex(srv, ":"); i != -1 {
			srv = srv[:i]
		}
		srv = fmt.Sprintf("%s:%d", srv, *port)
	}
	if *port == 6697 {
		*useTLS = true
	}

	var autojoin []string
	if *joinFlag != "" {
		for _, c := range strings.Split(*joinFlag, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				autojoin = append(autojoin, c)
			}
		}
	}

	cfg := ui.Config{
		Server:        srv,
		UseTLS:        *useTLS,
		TLSSkipVerify: *insecure,
		Nick:          n,
		AltNick:       alt,
		User:          un,
		Realname:      *realName,
		Password:      *password,
		AutoJoin:      autojoin,
		AutoConnect:   srv != "",
		Version:       version,
		SASLMechanism: strings.ToUpper(*sasl),
		SASLUser:      *saslUser,
		SASLPass:      *saslPass,
		CertFile:      *certFile,
		KeyFile:       *keyFile,
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
