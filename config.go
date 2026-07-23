package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type startupOptions struct {
	Server        string
	Port          int
	UseTLS        bool
	TLSSkipVerify bool
	CertFile      string
	KeyFile       string
	SASLMechanism string
	SASLUser      string
	SASLPass      string
	Nick          string
	AltNick       string
	User          string
	Realname      string
	Password      string
	AutoJoin      []string
}

type yamlConfig struct {
	Server        *string         `yaml:"server"`
	Port          *int            `yaml:"port"`
	UseTLS        *bool           `yaml:"tls"`
	TLSSkipVerify *bool           `yaml:"tls-insecure"`
	CertFile      *string         `yaml:"cert"`
	KeyFile       *string         `yaml:"key"`
	SASLMechanism *string         `yaml:"sasl"`
	SASLUser      *string         `yaml:"sasl-user"`
	SASLPass      *string         `yaml:"sasl-pass"`
	Nick          *string         `yaml:"nick"`
	AltNick       *string         `yaml:"altnick"`
	User          *string         `yaml:"user"`
	Realname      *string         `yaml:"realname"`
	Password      *string         `yaml:"password"`
	AutoJoin      *yamlStringList `yaml:"join"`
}

type yamlStringList []string

func (l *yamlStringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*l = splitList(value.Value)
		return nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("join entries must be strings")
			}
			entry := strings.TrimSpace(item.Value)
			if entry != "" {
				out = append(out, entry)
			}
		}
		*l = out
		return nil
	default:
		return fmt.Errorf("join must be a string or list of strings")
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "beacon", "config.yaml")
}

func loadStartupOptions(path string, requireFile bool, explicit map[string]bool, cli startupOptions) (startupOptions, error) {
	var opts startupOptions
	if path != "" {
		fileCfg, loaded, err := readYAMLConfig(path, requireFile)
		if err != nil {
			return startupOptions{}, err
		}
		if loaded {
			applyYAMLConfig(&opts, fileCfg)
		}
	}
	applyCLIOptions(&opts, cli, explicit)
	applyStartupDefaults(&opts)
	return opts, nil
}

func readYAMLConfig(path string, requireFile bool) (yamlConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !requireFile {
			return yamlConfig{}, false, nil
		}
		return yamlConfig{}, false, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg yamlConfig
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return yamlConfig{}, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, true, nil
}

func applyYAMLConfig(opts *startupOptions, cfg yamlConfig) {
	if cfg.Server != nil {
		opts.Server = *cfg.Server
	}
	if cfg.Port != nil {
		opts.Port = *cfg.Port
	}
	if cfg.UseTLS != nil {
		opts.UseTLS = *cfg.UseTLS
	}
	if cfg.TLSSkipVerify != nil {
		opts.TLSSkipVerify = *cfg.TLSSkipVerify
	}
	if cfg.CertFile != nil {
		opts.CertFile = *cfg.CertFile
	}
	if cfg.KeyFile != nil {
		opts.KeyFile = *cfg.KeyFile
	}
	if cfg.SASLMechanism != nil {
		opts.SASLMechanism = *cfg.SASLMechanism
	}
	if cfg.SASLUser != nil {
		opts.SASLUser = *cfg.SASLUser
	}
	if cfg.SASLPass != nil {
		opts.SASLPass = *cfg.SASLPass
	}
	if cfg.Nick != nil {
		opts.Nick = *cfg.Nick
	}
	if cfg.AltNick != nil {
		opts.AltNick = *cfg.AltNick
	}
	if cfg.User != nil {
		opts.User = *cfg.User
	}
	if cfg.Realname != nil {
		opts.Realname = *cfg.Realname
	}
	if cfg.Password != nil {
		opts.Password = *cfg.Password
	}
	if cfg.AutoJoin != nil {
		opts.AutoJoin = []string(*cfg.AutoJoin)
	}
}

func applyCLIOptions(opts *startupOptions, cli startupOptions, explicit map[string]bool) {
	if explicit["server"] {
		opts.Server = cli.Server
	}
	if explicit["port"] {
		opts.Port = cli.Port
	}
	if explicit["tls"] {
		opts.UseTLS = cli.UseTLS
	}
	if explicit["tls-insecure"] {
		opts.TLSSkipVerify = cli.TLSSkipVerify
	}
	if explicit["cert"] {
		opts.CertFile = cli.CertFile
	}
	if explicit["key"] {
		opts.KeyFile = cli.KeyFile
	}
	if explicit["sasl"] {
		opts.SASLMechanism = cli.SASLMechanism
	}
	if explicit["sasl-user"] {
		opts.SASLUser = cli.SASLUser
	}
	if explicit["sasl-pass"] {
		opts.SASLPass = cli.SASLPass
	}
	if explicit["nick"] {
		opts.Nick = cli.Nick
	}
	if explicit["altnick"] {
		opts.AltNick = cli.AltNick
	}
	if explicit["user"] {
		opts.User = cli.User
	}
	if explicit["realname"] {
		opts.Realname = cli.Realname
	}
	if explicit["password"] {
		opts.Password = cli.Password
	}
	if explicit["join"] {
		opts.AutoJoin = cli.AutoJoin
	}
}

func applyStartupDefaults(opts *startupOptions) {
	if opts.Nick == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			opts.Nick = sanitizeNick(u.Username)
		}
	}
	if opts.Nick == "" {
		opts.Nick = "beacon"
	}
	if opts.User == "" {
		opts.User = opts.Nick
	}
	if opts.AltNick == "" {
		opts.AltNick = opts.Nick + "_"
	}
	if opts.Realname == "" {
		opts.Realname = "beacon user"
	}
	if opts.Port == 6697 {
		opts.UseTLS = true
	}
	if opts.Server != "" && opts.Port != 0 {
		opts.Server = serverWithPort(opts.Server, opts.Port)
	}
	opts.SASLMechanism = strings.ToUpper(opts.SASLMechanism)
}

func explicitFlags(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		out[f.Name] = true
	})
	return out
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func serverWithPort(server string, port int) string {
	if i := strings.LastIndex(server, ":"); i != -1 {
		server = server[:i]
	}
	return fmt.Sprintf("%s:%d", server, port)
}
