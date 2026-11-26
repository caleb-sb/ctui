package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Root         string   `toml:"root"`          // dir holding main/ and the ticket worktrees
	Main         string   `toml:"main"`          // name of the main worktree dir under Root
	TmuxSession  string   `toml:"tmux_session"`  // tmux session that holds the dashboard + Claude windows
	ClaudeCmd    string   `toml:"claude_cmd"`    // claude binary
	ClaudeArgs   []string `toml:"claude_args"`   // extra args for every launch
	TicketPrefix string   `toml:"ticket_prefix"` // typing "5170" becomes "PROJ-5170"
	StartPrompt  string   `toml:"start_prompt"`  // first prompt for a new ticket
	FinishPrompt string   `toml:"finish_prompt"` // sent by `f`
}

const defaultStartPrompt = `We're starting work on {ticket}. Read context.md, then fetch {ticket} from Jira. ` +
	`Once you've read it, run {ctui} blurb {ticket} "<one-line summary of the ticket, under 60 characters>" so it shows on my dashboard. ` +
	`Decide which submodules the change touches and use scripts/new-feature.sh to create the {ticket} worktree ` +
	`with those submodules checked out. Then summarize the ticket and your plan, and wait for my go-ahead before changing code.`

const defaultFinishPrompt = `We're wrapping up {ticket}. Check that everything in the {ticket} worktree is committed and pushed ` +
	`and tell me if anything isn't. If it's all clean, run scripts/finish-feature.sh to tear down the {ticket} worktree.`

func defaultConfig() Config {
	return Config{
		Root:         "~/project",
		Main:         "main",
		TmuxSession:  "claude",
		ClaudeCmd:    "claude",
		TicketPrefix: "PROJ",
		StartPrompt:  defaultStartPrompt,
		FinishPrompt: defaultFinishPrompt,
	}
}

func configPath() string {
	if p := os.Getenv("CTUI_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home(), ".config")
	}
	return filepath.Join(base, "ctui", "config.toml")
}

func loadConfig() (Config, error) {
	c := defaultConfig()
	if _, err := toml.DecodeFile(configPath(), &c); err != nil && !os.IsNotExist(err) {
		return c, fmt.Errorf("config %s: %w", configPath(), err)
	}
	c.Root = expandHome(c.Root)
	return c, nil
}

func (c Config) MainDir() string { return filepath.Join(c.Root, c.Main) }

func (c Config) Prompt(tmpl, ticket string) string {
	r := strings.NewReplacer(
		"{ticket}", ticket,
		"{worktree}", filepath.Join(c.Root, ticket),
		"{root}", c.Root,
		"{ctui}", shellQuote(selfPath()),
	)
	return r.Replace(tmpl)
}

// NormalizeTicket turns "5170" / "proj-5170" into "PROJ-5170".
func (c Config) NormalizeTicket(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "", fmt.Errorf("empty ticket")
	}
	if isDigits(s) && c.TicketPrefix != "" {
		s = strings.ToUpper(c.TicketPrefix) + "-" + s
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("invalid ticket %q", s)
		}
	}
	return s, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func selfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "ctui"
	}
	return exe
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func expandHome(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home(), p[2:])
	}
	return p
}
