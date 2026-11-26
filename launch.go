package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// launch opens a tmux window in the main worktree running Claude for ticket.
// With resumeID set it resumes that conversation. Otherwise it starts a new one with a fresh session ID.
// prompt is optional.
func launch(cfg Config, ticket, prompt, resumeID string) (Pane, error) {
	settings, err := writeHooksFile()
	if err != nil {
		return Pane{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Pane{}, err
	}
	sid := resumeID
	argv := append([]string{cfg.ClaudeCmd}, cfg.ClaudeArgs...)
	argv = append(argv, "--settings", settings, "-n", ticket)
	if resumeID != "" {
		argv = append(argv, "--resume", resumeID)
	} else {
		sid = newUUID()
		argv = append(argv, "--session-id", sid)
	}
	if prompt != "" {
		argv = append(argv, prompt)
	}
	if _, err := updateState(ticket, func(s *State) {
		s.State, s.Msg, s.Unread = StStarting, "starting", false
		s.SessionID, s.Argv = sid, argv
	}); err != nil {
		return Pane{}, err
	}

	// The window runs `ctui _exec TICKET`, which reads argv from the state file.
	// That keeps the prompt clear of shell quoting (which differs between shells such as fish and sh).
	shellCmd := shellQuote(exe) + " _exec " + ticket
	sess := cfg.TmuxSession
	format := "#{pane_id}\t#{window_id}"
	env := "-eCTUI_STATE_DIR=" + stateDir()
	var out string
	if hasSession(sess) {
		out, err = tmux("new-window", "-d", "-t", "="+sess+":", "-n", ticket, "-c", cfg.MainDir(), env, "-P", "-F", format, shellCmd)
	} else {
		out, err = tmux("new-session", "-d", "-s", sess, "-n", ticket, "-c", cfg.MainDir(), env, "-P", "-F", format, shellCmd)
	}
	if err != nil {
		return Pane{}, err
	}
	f := strings.Split(strings.TrimSpace(out), "\t")
	if len(f) != 2 {
		return Pane{}, fmt.Errorf("unexpected tmux output %q", out)
	}
	p := Pane{Ticket: ticket, ID: f[0], Window: f[1], Session: sess}
	if _, err := tmux("set-option", "-p", "-t", p.ID, "@ctui_ticket", ticket); err != nil {
		return p, err
	}
	_, _ = tmux("set-option", "-w", "-t", p.Window, "@ctui_state", StStarting)
	return p, nil
}

// runExec is the `ctui _exec TICKET` subcommand that runs inside the tmux window.
func runExec(ticket string) int {
	st := readState(ticket)
	if len(st.Argv) == 0 {
		fmt.Fprintf(os.Stderr, "ctui: no launch command recorded for %s\n", ticket)
		waitEnter()
		return 1
	}
	os.Setenv("CTUI_TICKET", ticket)
	os.Setenv("CTUI_STATE_DIR", stateDir())

	// Catch SIGINT, don't ignore it: a caught signal is reset to the default in the child,
	// so Claude handles Ctrl-C normally and the wrapper doesn't die on it.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)

	cmd := exec.Command(st.Argv[0], st.Argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	_, _ = updateState(ticket, func(s *State) { s.State = StExited })
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nctui: claude exited: %v\n", err)
		waitEnter()
		return 1
	}
	return 0
}

func waitEnter() {
	fmt.Fprint(os.Stderr, "press Enter to close this window")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// runDashboard runs when ctui starts outside tmux. It makes sure the session and its
// dashboard window exist, then attaches.
func runDashboard(cfg Config) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tuiCmd := shellQuote(exe) + " tui"
	sess := cfg.TmuxSession
	var win string
	if !hasSession(sess) {
		out, err := tmux("new-session", "-d", "-s", sess, "-n", "dash", "-c", cfg.MainDir(), "-P", "-F", "#{window_id}", tuiCmd)
		if err != nil {
			return err
		}
		win = strings.TrimSpace(out)
	} else {
		win = findDash(sess)
		if win == "" {
			out, err := tmux("new-window", "-t", "="+sess+":", "-n", "dash", "-c", cfg.MainDir(), "-P", "-F", "#{window_id}", tuiCmd)
			if err != nil {
				return err
			}
			win = strings.TrimSpace(out)
		}
	}
	_, _ = tmux("set-option", "-w", "-t", win, "@ctui_dash", "1")
	_, _ = tmux("select-window", "-t", win)
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", "=" + sess}, os.Environ())
}

func findDash(sess string) string {
	out, err := tmux("list-windows", "-t", "="+sess, "-F", "#{window_id}\t#{@ctui_dash}")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 2 && f[1] == "1" {
			return f[0]
		}
	}
	return ""
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
