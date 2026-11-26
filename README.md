# ctui

A tmux dashboard for running one Claude Code session per ticket against a git superrepo that uses worktrees.

```
╭─ ctui  ~/project ────────────────────────────────────────────────────────────────── ! 1  ● 1  ⠹ 1 ─╮
│ ▌ ! BUG    PROJ-5166  start › repro › [ verify ] › spec › tests › fix › checks › mr › demo › finish │
│   ● FEAT   PROJ-5175  … › tests › [ implement ◆ ] › checks › mr › demo › finish                     │
│   ⠹ REVIEW PROJ-5170  start › plan › [ review ] › finish                                            │
│   ·        PROJ-5160                                                                                │
╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ PROJ-5166-fix-rounding ────────────────────────────────────────────────────────────────────── BUG ─╮
│ Fix rounding on prorated invoices                                                                   │
│ repos    web-app                                                                                    │
│ worktree ~/project/PROJ-5166-fix-rounding                                                           │
╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ session ───────────────────────────────────────────────────────────────────────────────────────────╮
│ (live view of the selected session's screen)                                                        │
╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
 j/k move  ·  enter open  ·  n new  ·  i reply  ·  f finish  ·  x kill  ·  ? help
```

The top panel lists tickets with their status icon, work type and stage pipeline. The middle panel
shows the selected ticket's blurb, checked-out submodules and worktree. The bottom panel is its live screen.

Worktrees are expected to be named `<TICKET>-<slug>` (for example `PROJ-5166-fix-rounding`), with the
branch named the same. ctui takes the ticket from the front of the name.

The blurb is written by Claude: the default `start_prompt` asks it to run
`ctui blurb TICKET "<summary>"` after it reads the Jira ticket. ctui-launched sessions are allowed to
run it (and `ctui type` / `ctui stage`) without a permission prompt (the rule is in the generated `--settings` file).
You can also run `ctui blurb` yourself to change it.

## Work types and stages

Each kind of work (by default `bug`, `feat` and `review`) has a colour and a list of stages, set in
`config.toml` under `[types.<name>]`. After reading the ticket, Claude picks one with
`ctui type TICKET bug`. That prints the stages and the type's `instructions` (for example, which skill
to use). Claude then runs `ctui stage TICKET <stage>` as it enters each stage.

- The pipeline shows finished stages dimmed, the current one `[ bracketed ]` and bold, and the rest dim grey.
- Reporting a later stage marks the ones in between as skipped (struck through). Reporting an earlier
  one goes back to it.
- `approve` is a stage where the work waits for your approval of the stage before it. It isn't shown as
  its own step: the approved stage shows a pulsing diamond, `[ implement ◆ ]`.
- After `/clear`, a resume or a compaction, the SessionStart hook reminds Claude of the type, the
  stages and where it is, so it keeps reporting.
- A worktree whose branch has `branch.<branch>.review = true` in the superrepo's git config shows as
  REVIEW even without a ctui session (with no pipeline, since its stage isn't known).

The pulse uses 24-bit colour. Inside tmux, ctui always sends it and tmux converts it for terminals that
can't show it. Tell tmux your terminal can, for example `set -ag terminal-overrides ",xterm-256color:RGB"`,
or the fade looks stepped.

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
| `f` | send `finish_prompt` (Claude checks it's pushed, then runs `finish-feature`) |
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
  the worktree is created, press `n` with the same ticket to start again.
