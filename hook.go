package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hook events registered through --settings on every Claude that ctui launches.
var hookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PermissionRequest", "Notification", "Stop", "SubagentStop", "PreCompact", "SessionEnd",
}

// Tools that stop and wait for the user even though no permission prompt fires.
var askTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

type hookPayload struct {
	Event            string          `json:"hook_event_name"`
	SessionID        string          `json:"session_id"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	LastAssistant    string          `json:"last_assistant_message"`
	Source           string          `json:"source"` // SessionStart: startup, resume, clear, compact
}

// writeHooksFile writes the settings JSON passed to `claude --settings`.
func writeHooksFile() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cmd := shellQuote(exe) + " hook"
	hooks := map[string]any{}
	for _, ev := range hookEvents {
		entry := map[string]any{
			"hooks": []map[string]any{{"type": "command", "command": cmd, "timeout": 10}},
		}
		if ev == "PreToolUse" || ev == "PostToolUse" {
			entry["matcher"] = "*"
		}
		hooks[ev] = []any{entry}
	}
	settings := map[string]any{
		"hooks": hooks,
		// Let Claude set the blurb, type and stage without a permission prompt. This only applies to ctui-launched sessions.
		"permissions": map[string]any{"allow": []string{
			"Bash(" + exe + " blurb:*)", "Bash(" + exe + " type:*)", "Bash(" + exe + " stage:*)",
		}},
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	path := filepath.Join(stateDir(), "hooks.json")
	_ = os.MkdirAll(stateDir(), 0o755)
	if old, err := os.ReadFile(path); err == nil && string(old) == string(b) {
		return path, nil
	}
	return path, os.WriteFile(path, b, 0o644)
}

// runHook is the `ctui hook` subcommand. Stdout from some hooks becomes Claude context, so it
// prints nothing except the stage reminder on SessionStart. It must never fail in a way that blocks Claude.
func runHook() {
	defer func() { _ = recover() }()
	ticket := os.Getenv("CTUI_TICKET")
	if ticket == "" {
		return
	}
	var p hookPayload
	_ = json.NewDecoder(os.Stdin).Decode(&p)
	pane := os.Getenv("TMUX_PANE")

	var state, msg string
	setMsg := false
	unread := false
	tool := ""
	switch p.Event {
	case "SessionStart":
		state, msg, setMsg = StIdle, "", true
	case "UserPromptSubmit":
		state, msg, setMsg = StWorking, "thinking", true
	case "PreToolUse":
		if askTools[p.ToolName] {
			state, msg, setMsg = StWaiting, p.ToolName, true
		} else {
			state, msg, setMsg = StWorking, toolSummary(p.ToolName, p.ToolInput), true
			tool = msg
		}
	case "PermissionRequest":
		// Fires as soon as the dialog opens. The permission_prompt Notification only comes several seconds later.
		state, msg, setMsg = StWaiting, "permission: "+toolSummary(p.ToolName, p.ToolInput), true
	case "PostToolUse":
		state = StWorking
	case "SubagentStop":
		// Background helper agents can finish after the main turn's Stop, so this says
		// nothing about the main session. Just record the event.
	case "PreCompact":
		state, msg, setMsg = StWorking, "compacting", true
	case "Notification":
		m := strings.ToLower(p.Message)
		switch {
		case p.NotificationType == "permission_prompt" || p.NotificationType == "elicitation_dialog" ||
			(p.NotificationType == "" && strings.Contains(m, "permission")):
			// This is the delayed fallback. Ignore it once the turn has ended, and don't
			// overwrite the more specific PermissionRequest message.
			cur := readState(ticket).State
			if cur == StIdle || cur == StExited {
				return
			}
			state = StWaiting
			if cur != StWaiting {
				msg, setMsg = p.Message, true
			}
		case p.NotificationType == "idle_prompt" || strings.Contains(m, "waiting for your input"):
			// Reminder that Claude is idle. Keep the state and the unread flag as they are.
			cur := readState(ticket).State
			if cur == "" || cur == StWorking {
				state = StIdle
			}
		default:
			return
		}
	case "Stop":
		state, setMsg = StIdle, true
		msg = firstLine(p.LastAssistant)
		if msg == "" {
			msg = "done"
		}
		unread = !paneVisible(pane)
	case "SessionEnd":
		state = StExited
	default:
		return
	}

	_, _ = updateState(ticket, func(s *State) {
		if state != "" {
			s.State = state
		}
		if setMsg {
			s.Msg = msg
		}
		if tool != "" {
			s.Tool = tool
		}
		s.Event = p.Event
		if p.SessionID != "" {
			s.SessionID = p.SessionID
		}
		if pane != "" {
			s.Pane = pane
		}
		switch state {
		case StIdle:
			if p.Event == "Stop" {
				s.Unread = unread
			}
		case StWorking, StWaiting, StExited:
			s.Unread = false
		}
	})
	if pane != "" && state != "" {
		_, _ = tmux("set-option", "-w", "-t", pane, "@ctui_state", state)
	}
	if p.Event == "SessionStart" {
		printStageReminder(ticket)
	}
}

// printStageReminder tells a session that already has a work type (after /clear, a resume or a
// compaction) to keep reporting stages. SessionStart hook stdout is added to Claude's context.
func printStageReminder(ticket string) {
	st := readState(ticket)
	cfg, err := loadConfig()
	if err != nil || st.Type == "" {
		return
	}
	wt, ok := cfg.workType(st.Type)
	if !ok {
		return
	}
	fmt.Printf("This session is working on %s, which is %s work, and reports its progress to the ctui dashboard.\n%s\n",
		ticket, strings.ToUpper(st.Type), cfg.stageGuide(ticket, wt, st.Stage))
}

func toolSummary(tool string, raw json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	detail := ""
	switch tool {
	case "Bash":
		detail = str("command")
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		detail = filepath.Base(str("file_path"))
	case "Grep", "Glob":
		detail = str("pattern")
	case "Task", "Agent":
		detail = str("description")
	case "WebFetch":
		detail = str("url")
	case "WebSearch":
		detail = str("query")
	}
	if detail == "" {
		return tool
	}
	return fmt.Sprintf("%s: %s", tool, firstLine(detail))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
