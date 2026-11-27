package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Root          string   `toml:"root"`           // dir holding main/ and the ticket worktrees
	Main          string   `toml:"main"`           // name of the main worktree dir under Root
	TmuxSession   string   `toml:"tmux_session"`   // tmux session that holds the dashboard + Claude windows
	ClaudeCmd     string   `toml:"claude_cmd"`     // claude binary
	ClaudeArgs    []string `toml:"claude_args"`    // extra args for every launch
	TicketPrefix  string   `toml:"ticket_prefix"`  // typing "5170" becomes "PROJ-5170"
	StartPrompt   string   `toml:"start_prompt"`   // first prompt for a new ticket
	ExplorePrompt string   `toml:"explore_prompt"` // first prompt for a new exploration (a name rather than a ticket)
	FinishPrompt  string   `toml:"finish_prompt"`  // sent by `f`

	TuicrCmd      string `toml:"tuicr_cmd"`      // tuicr to run for reviewing diffs (`d`); empty: the bundled one, else tuicr on PATH
	ReviewPrompt  string `toml:"review_prompt"`  // wraps the comments exported from tuicr
	ApprovePrompt string `toml:"approve_prompt"` // sent by `a` in the review picker

	Types map[string]WorkType `toml:"types"` // kinds of work, keyed by name (bug, feat, review)
}

const defaultStartPrompt = `We're starting work on {ticket}. Read context.md, then fetch {ticket} from Jira. ` +
	`Once you've read it, run {ctui} blurb {ticket} "<one-line summary of the ticket, under 60 characters>" so it shows on my dashboard. ` +
	`Then decide what kind of work it is and run {ctui} type {ticket} <{types}>. ` +
	`It prints the stages of that work and what to do next. Follow it.`

const defaultExplorePrompt = `We're starting an exploration called {ticket}. It has no Jira ticket. Read context.md, ` +
	`then run {ctui} type {ticket} explore. It prints what to do next. Follow it. What we're exploring: {brief}`

const defaultFinishPrompt = `We're wrapping up {ticket}. Check that everything in the {ticket} worktree is committed and pushed ` +
	`and tell me if anything isn't. If it's all clean, run {ctui} stage {ticket} finish, then use scripts/finish-feature ` +
	`to tear down the {ticket} worktree.`

const defaultReviewPrompt = `My review comments on {repo} in the {ticket} worktree. File paths are relative to {repo_path}.

{comments}`

const defaultApprovePrompt = `Approved, carry on.`

func defaultConfig() Config {
	return Config{
		Root:          "~/project",
		Main:          "main",
		TmuxSession:   "claude",
		ClaudeCmd:     "claude",
		TicketPrefix:  "PROJ",
		StartPrompt:   defaultStartPrompt,
		ExplorePrompt: defaultExplorePrompt,
		FinishPrompt:  defaultFinishPrompt,
		ReviewPrompt:  defaultReviewPrompt,
		ApprovePrompt: defaultApprovePrompt,
		Types:         defaultTypes(),
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
	// A [types.X] table in the file replaces the default type X; other defaults stay.
	if _, err := toml.DecodeFile(configPath(), &c); err != nil && !os.IsNotExist(err) {
		return c, fmt.Errorf("config %s: %w", configPath(), err)
	}
	c.Root = expandHome(c.Root)
	return c, nil
}

func (c Config) MainDir() string { return filepath.Join(c.Root, c.Main) }

func (c Config) Prompt(tmpl, ticket string) string {
	// {worktree} is the ticket's actual worktree (root/<TICKET>-<slug>) once it exists.
	worktree := filepath.Join(c.Root, ticket)
	if strings.Contains(tmpl, "{worktree}") {
		if wt, ok := listWorktrees(c)[ticket]; ok {
			worktree = wt.Path
		}
	}
	r := strings.NewReplacer(
		"{ticket}", ticket,
		"{worktree}", worktree,
		"{root}", c.Root,
		"{ctui}", shellQuote(selfPath()),
		"{types}", strings.Join(c.typeNames(), "|"),
	)
	return r.Replace(tmpl)
}

// NormalizeTicket turns "5170" / "proj-5170" into "PROJ-5170". Anything else that isn't a ticket
// is an exploration name, kept lowercase: "Login-Mockups" gives "login-mockups".
func (c Config) NormalizeTicket(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty ticket")
	}
	if isDigits(s) && c.TicketPrefix != "" {
		s = c.TicketPrefix + "-" + s
	}
	if isTicket(s) {
		return strings.ToUpper(s), nil
	}
	if ticketKey.MatchString(s) {
		// A worktree dir with this name would be taken for the ticket at its front.
		return "", fmt.Errorf("%q starts like a ticket; give the ticket alone, or another name", s)
	}
	s = strings.ToLower(s)
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("invalid ticket or name %q", s)
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
