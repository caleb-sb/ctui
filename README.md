# ctui

A tmux dashboard for running one Claude Code session per ticket against a git superrepo that uses worktrees.

```
╭─ ctui  ~/project ─────────────────────────────────────── ! 1  ● 1  ⠹ 1 ─╮
│ ▌ ! PROJ-5166                                                           │
│   ● PROJ-5170                                                           │
│   ⠹ PROJ-5167                                                           │
│   · PROJ-5160                                                           │
╰─────────────────────────────────────────────────────────────────────────╯
╭─ PROJ-5166 ─────────────────────────────────────────────────────────────╮
│ Fix rounding on prorated invoices                                       │
│ repos    web-app                                                        │
│ branch   PROJ-5166  ·  ~/project/PROJ-5166                              │
╰─────────────────────────────────────────────────────────────────────────╯
╭─ session ───────────────────────────────────────────────────────────────╮
│ (live view of the selected session's screen)                            │
╰─────────────────────────────────────────────────────────────────────────╯
 j/k move  ·  enter open  ·  n new  ·  i reply  ·  f finish  ·  x kill  ·  ? help
```

The top panel lists tickets with their status icon. The middle panel shows the selected ticket's
blurb, checked-out submodules, branch and worktree. The bottom panel is its live screen.

The blurb is written by Claude: the default `start_prompt` asks it to run
`ctui blurb TICKET "<summary>"` after it reads the Jira ticket. ctui-launched sessions are allowed to
run that one command without a permission prompt (the rule is in the generated `--settings` file).
You can also run `ctui blurb` yourself to change it.

## Install

Download the binary for your platform from the
[latest release](https://github.com/caleb-sb/ctui/releases/latest) and put it on `PATH`:

```sh
# macOS, Apple silicon
curl -fL -o ~/.local/bin/ctui https://github.com/caleb-sb/ctui/releases/latest/download/ctui-darwin-arm64
# Linux, x86-64
curl -fL -o ~/.local/bin/ctui https://github.com/caleb-sb/ctui/releases/latest/download/ctui-linux-amd64

chmod +x ~/.local/bin/ctui
mkdir -p ~/.config/ctui && curl -fL -o ~/.config/ctui/config.toml \
  https://raw.githubusercontent.com/caleb-sb/ctui/main/config.example.toml   # then edit root, ticket_prefix, prompts
```

The binaries aren't notarized. On macOS a file downloaded with `curl` runs as-is. If you download it
in a browser, clear the quarantine flag first: `xattr -d com.apple.quarantine ~/.local/bin/ctui`.

Requires tmux (3.0+) and Claude Code. To build from source (Go 1.27+):
`CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o ctui .`

## Use

- `ctui` outside tmux creates or attaches the `claude` tmux session, with the dashboard in a `dash` window.
- `ctui --popup` runs once and quits after you open a session. It's meant for a tmux popup.

Suggested `~/.tmux.conf` bindings:

```tmux
# prefix C-c: dashboard as a popup from anywhere
bind-key C-c display-popup -E -w 90% -h 85% "ctui --popup"
# prefix C-d: jump back to the dashboard window
bind-key C-d run-shell "tmux select-window -t '=claude:dash' && tmux switch-client -t '=claude'"
```

| key | action |
|---|---|
| `j`/`k`, `g`/`G` | move |
| `enter`/`l` | open the session. If Claude isn't running, it resumes the ticket's last conversation. |
| `n` | new session. Type `5170` or `PROJ-5170` and Claude starts in `main/` with `start_prompt`. |
| `i` | send a reply without switching (blocked while a prompt is showing) |
| `f` | send `finish_prompt` (Claude checks it's pushed, then runs `finish-feature.sh`) |
| `x` | kill the Claude window (keeps the worktree) |
| `r`, `?`, `q` | refresh, help, quit (`esc` also quits in popup mode) |

The list shows every worktree of the superrepo (except `main`) plus every running ctui session.

## How status works

ctui launches Claude with `--settings ~/.local/state/ctui/hooks.json`, which adds hooks that call
`ctui hook`. Your global `~/.claude/settings.json` isn't touched, and sessions you start by hand
aren't tracked. The hooks write `~/.local/state/ctui/tickets/<TICKET>.json`.

- **working**: `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PreCompact`
- **waiting**: `PermissionRequest` (immediate), the `permission_prompt` notification (a few seconds later, as a fallback),
  and `AskUserQuestion` / `ExitPlanMode`
- **done**: `Stop` while you're not looking at that window. It becomes **idle** once you view the window.

No hook fires when you answer a permission prompt or press Esc to interrupt, so the dashboard also
checks the pane for Claude's `esc to interrupt` status text. If that text is showing, the session is
working. If it has been missing for 20s while the hooks still say working, the row shows **idle?**.

Each launch records the Claude session ID (and keeps it updated after `/clear`), so selecting a
worktree with no Claude running runs `claude --resume <id>`.

## Notes

- Tested with Claude Code 2.1.332 and tmux 3.8. The `PermissionRequest` hook needs a recent Claude
  Code, so update it if it's older.
- `<root>/main` must already be trusted by Claude. Otherwise the first launch sits on the
  folder-trust dialog and shows as "starting".
- If auto mode is on globally, most tool calls never ask for permission, so you'll see "waiting" less.
- A ticket appears only while it has a worktree or a running window. If Claude dies before
  `new-feature.sh` creates the worktree, press `n` with the same ticket to start again.
