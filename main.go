// ctui is a tmux dashboard for per-ticket Claude Code sessions.
package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `usage:
  ctui            open the dashboard (attaches the tmux session if you're outside tmux)
  ctui --popup    run the TUI once, e.g. from tmux display-popup; quits after you open a session
  ctui blurb TICKET TEXT   set the one-line ticket summary shown on the dashboard
  ctui hook       Claude Code hook handler (installed automatically through --settings)
`

func main() {
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	switch arg {
	case "hook":
		runHook()
		return
	case "blurb":
		os.Exit(runBlurb(os.Args[2:]))
	case "_exec":
		if len(os.Args) < 3 {
			os.Exit(2)
		}
		os.Exit(runExec(os.Args[2]))
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		os.Exit(1)
	}
	switch arg {
	case "":
		if !inTmux() {
			err = runDashboard(cfg)
		} else {
			err = runTUI(cfg, false)
		}
	case "tui":
		err = runTUI(cfg, false)
	case "--popup":
		err = runTUI(cfg, true)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		os.Exit(1)
	}
}

func runBlurb(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ctui blurb TICKET TEXT")
		return 2
	}
	cfg, _ := loadConfig()
	ticket, err := cfg.NormalizeTicket(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 2
	}
	text := firstLine(strings.Join(args[1:], " "))
	if _, err := updateState(ticket, func(s *State) { s.Blurb = text }); err != nil {
		fmt.Fprintln(os.Stderr, "ctui:", err)
		return 1
	}
	fmt.Printf("blurb for %s set: %s\n", ticket, text)
	return 0
}
