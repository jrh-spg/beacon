# beacon

A terminal IRC client in Go, written in the spirit of BitchX and irssi,
with native TLS, SASL support, DCC transfers, and a BitchX-inspired terminal UI.

```
  ____
 | __ )  ___  __ _  ___ ___  _ __
 |  _ \ / _ \/ _` |/ __/ _ \| '_ \
 | |_) |  __/ (_| | (_| (_) | | | |
 |____/ \___|\__,_|\___\___/|_| |_|
  -=> a BitchX-inspired irc client <=-
```

## Build

```sh
go build -o beacon .
```

A binary called `beacon` is produced in the current directory.

## Quick start

```sh
# Connect via TLS on the default 6697
./beacon -server irc.libera.chat -tls -nick myhandle -join '#grbx'

# Plain text, default 6667
./beacon -server irc.example.org -nick rocknrolla

# Launch with no connection, then use /server inside
./beacon
```

### Flags

| Flag             | Meaning                                                |
|------------------|--------------------------------------------------------|
| `-server`        | host (with optional `:port`) to connect to at start    |
| `-port`          | port override (sets TLS automatically if 6697)         |
| `-tls`           | use TLS for the initial server                         |
| `-tls-insecure`  | skip TLS certificate verification (use carefully)      |
| `-cert`          | PEM client certificate for SASL EXTERNAL / CertFP      |
| `-key`           | PEM private key for `-cert` if not in the same PEM     |
| `-sasl`          | SASL mechanism: `EXTERNAL` or `PLAIN`                  |
| `-sasl-user`     | SASL PLAIN username                                    |
| `-sasl-pass`     | SASL PLAIN password                                    |
| `-nick`          | nickname (defaults to `$USER` or `beacon`)             |
| `-altnick`       | alternate nick if primary is taken                     |
| `-user`          | ident username (defaults to nick)                      |
| `-realname`      | GECOS / real name                                      |
| `-password`      | server password (`PASS`)                               |
| `-join`          | comma-separated channels to auto-join on connect       |
| `-version`       | print version and exit                                 |

## In-client commands

Type `/help` once running. Highlights:

```
/server <host>[:port] [tls]   connect (alias /connect)
/sslserver <host>[:port]      connect with TLS (alias /ssl)
/disconnect [reason]          drop connection
/quit [reason]                quit beacon
/nick <name>                  change nick
/join <#chan> [key]           join (alias /j)
/part [#chan] [reason]        leave (alias /leave)
/cycle [#chan]                part + rejoin (alias /hop)
/close                        close current window (alias /wc)
/msg <target> <text>          PM (alias /query)
/notice <target> <text>       NOTICE
/me <action>                  ACTION
/topic [#chan] <topic>        view or set topic
/mode <target> <modes>        set modes
/op <nick>...                 give ops (also /deop, /voice, /devoice)
/ban <mask>                   set ban mask (also /unban)
/invite <nick> [#chan]        invite nick to channel
/kick <nick> [reason]         kick (in current channel)
/whois <nick>                 whois
/who [target]                 WHO query
/names [#chan]                list nicks
/list [pattern]               LIST channels
/wallops <text>               send WALLOPS
/away [msg]                   set / unset away
/raw <line>                   send raw line (alias /quote)
/window <n|name|next|prev>    switch window (alias /win)
/buffers                      list windows
/clear                        clear current window
/lastlog <substring>          search current window scrollback
/echo <text>                  print local text in current window
/eval <cmd>;<cmd>;...         run multiple commands separated by ;
/ctcp <target> <type> [args]  send a CTCP request
/ping <target>                CTCP PING with round-trip timing
/version [target]             show version or request CTCP VERSION
/uptime                       show runtime since launch
/date | /time                 show local date/time
/set [key [value]]            view or change runtime settings
/toggle <key>                 toggle a boolean setting
/ignore [add|del|list] [nick] manage ignored nicks
/autojoin [add|del|list] [#chan]  manage saved auto-joined channels
/dcc list|send|chat|accept|close  DCC transfers and chat
```

`/server +host` or `/server host:+6697` toggles TLS the irssi way.

## Runtime settings

`/set` lists settings grouped by category. `/set <category>` filters the list,
`/set <key>` shows one setting with its type and description, and
`/set <key> <value>` changes it for the current session. Boolean settings can
also be changed with `/toggle <key>`.

Settings cover reconnect behavior, default quit/part messages, alternate nick,
CTCP replies, timestamp format, nick completion, mention beeps, auto-rejoin,
DCC defaults, and logging defaults.

## Autojoin

`/autojoin add [#channel]` saves a channel to `~/.config/beacon/autojoin`.
`/autojoin del [#channel]` removes one, and `/autojoin list` prints the saved
list. When no channel is supplied, the active channel is used.

## DCC

beacon supports classic DCC `SEND` and `CHAT` over plain TCP:

```
/dcc list                       show pending offers and active transfers
/dcc accept <nick> [save-path]  accept the latest offer from a nick
/dcc send <nick> <file>         offer a file
/dcc chat <nick>                offer a DCC chat session
/dcc close <nick|id>            cancel an offer or transfer
```

Received files default to `~/Downloads`; change this with
`/set dcc_download_dir <path>`. Incoming offers can be accepted automatically
with `/set dcc_auto_accept on`.

## Keys

| Key                 | Action                       |
|---------------------|------------------------------|
| `PgUp` / `PgDn`     | scroll active window         |
| `Home` / `End`      | jump to top / re-enable autoscroll |
| `Ctrl-N` / `Ctrl-P` | next / previous window       |
| `Alt-1` … `Alt-9`   | jump to window N             |
| `Tab`               | nick completion              |
| `Up` / `Down`       | input history                |
| `Enter`             | send line                    |

To send a literal line that starts with `/`, double it: `//slashed`.

## Layout

```
┌─ title bar (topic / window kind) ─────────────────────┐
│                                                       │
│ active buffer scrollback (BitchX-inspired styling)    │
│                                                       │
├─ status bar [time] [nick/conn] [1:(status)] [2:#chan] ┤
│ [beacon] » _                                          │
└───────────────────────────────────────────────────────┘
```

## Notes

* TLS uses Go's `crypto/tls` with `MinVersion = TLS 1.2`. `-tls-insecure`
  disables verification — only use it when you have a good reason.
* SASL `PLAIN` and `EXTERNAL` are supported. Use `-cert`/`-key` with
  `-sasl EXTERNAL` for CertFP-style authentication.
* `PING` is auto-`PONG`'d. CTCP `VERSION`, `PING`, `TIME`, `CLIENTINFO`,
  `USERINFO`, `SOURCE`, and `FINGER` are answered.
* The UI palette uses 256-color tcell tags; a true-color terminal works best.
* IRCv3 capability negotiation is implemented for SASL. Other IRCv3 message
  tags are parsed and ignored.
