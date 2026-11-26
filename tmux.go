package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func tmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return out.String(), nil
}

func inTmux() bool { return os.Getenv("TMUX") != "" }

func hasSession(name string) bool {
	_, err := tmux("has-session", "-t", "="+name)
	return err == nil
}

// Pane is a tmux pane that ctui launched Claude in. It's tagged with the @ctui_ticket pane option.
type Pane struct {
	Ticket, ID, Window, Session string
	WindowActive, Attached      bool
	Dead                        bool
}

// Visible means you're looking at this pane's window right now.
func (p Pane) Visible() bool { return p.WindowActive && p.Attached }

func listPanes() map[string]Pane {
	out, err := tmux("list-panes", "-a", "-F",
		"#{@ctui_ticket}\t#{pane_id}\t#{window_id}\t#{session_name}\t#{window_active}\t#{session_attached}\t#{pane_dead}")
	res := map[string]Pane{}
	if err != nil {
		return res
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 7 || f[0] == "" {
			continue
		}
		res[f[0]] = Pane{
			Ticket: f[0], ID: f[1], Window: f[2], Session: f[3],
			WindowActive: f[4] == "1", Attached: f[5] != "0" && f[5] != "",
			Dead: f[6] == "1",
		}
	}
	return res
}

func paneVisible(pane string) bool {
	if pane == "" {
		return false
	}
	out, err := tmux("display-message", "-p", "-t", pane, "#{window_active} #{session_attached}")
	if err != nil {
		return false
	}
	f := strings.Fields(out)
	return len(f) == 2 && f[0] == "1" && f[1] != "0"
}

// capturePane returns the visible screen, with colours, trailing blank lines trimmed.
func capturePane(pane string) string {
	out, err := tmux("capture-pane", "-p", "-e", "-t", pane)
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n ")
}

func capturePlain(pane string) string {
	out, _ := tmux("capture-pane", "-p", "-t", pane)
	return out
}

// sendText types text into a pane as a bracketed paste (so newlines don't submit early), then presses Enter.
func sendText(pane, text string) error {
	cmd := exec.Command("tmux", "load-buffer", "-b", "ctui-send", "-")
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %s", strings.TrimSpace(string(out)))
	}
	if _, err := tmux("paste-buffer", "-p", "-d", "-b", "ctui-send", "-t", pane); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	_, err := tmux("send-keys", "-t", pane, "Enter")
	return err
}

func switchTo(window string) error {
	if _, err := tmux("select-window", "-t", window); err != nil {
		return err
	}
	if inTmux() {
		if _, err := tmux("switch-client", "-t", window); err != nil {
			return err
		}
	}
	return nil
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:=", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
