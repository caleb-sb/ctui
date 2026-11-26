package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Session states written by the hook.
const (
	StStarting = "starting"
	StWorking  = "working"
	StWaiting  = "waiting" // permission prompt / question for the user
	StIdle     = "idle"    // turn finished; Unread distinguishes "done" from "idle"
	StExited   = "exited"
)

type State struct {
	State     string    `json:"state"`
	Since     time.Time `json:"since"`   // when State last changed
	Updated   time.Time `json:"updated"` // last hook event
	Unread    bool      `json:"unread"`  // finished a turn you haven't looked at
	Blurb     string    `json:"blurb"`   // one-line ticket summary, written by Claude through `ctui blurb`
	Msg       string    `json:"msg"`
	Tool      string    `json:"tool"` // last tool started (shown when we infer "working" from the screen)
	Event     string    `json:"event"`
	SessionID string    `json:"session_id"`
	Pane      string    `json:"pane"`
	Argv      []string  `json:"argv"` // what _exec should run

	Type    string `json:"type,omitempty"`    // work type, set by Claude through `ctui type`
	Stage   int    `json:"stage"`             // index into the type's stages, set through `ctui stage`
	Skipped []int  `json:"skipped,omitempty"` // stage indexes that were jumped over
}

func stateDir() string {
	if d := os.Getenv("CTUI_STATE_DIR"); d != "" {
		return d
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home(), ".local", "state")
	}
	return filepath.Join(base, "ctui")
}

func ticketsDir() string {
	d := filepath.Join(stateDir(), "tickets")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func readState(ticket string) State {
	var s State
	b, err := os.ReadFile(filepath.Join(ticketsDir(), ticket+".json"))
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// updateState does a locked read-modify-write, so the hook and the TUI can't clobber each other.
func updateState(ticket string, fn func(*State)) (State, error) {
	dir := ticketsDir()
	lf, err := os.OpenFile(filepath.Join(dir, ticket+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return State{}, err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return State{}, err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	s := readState(ticket)
	prev := s.State
	fn(&s)
	now := time.Now()
	if s.State != prev || s.Since.IsZero() {
		s.Since = now
	}
	s.Updated = now
	b, _ := json.MarshalIndent(s, "", "  ")
	path := filepath.Join(dir, ticket+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return s, err
	}
	return s, os.Rename(tmp, path)
}
