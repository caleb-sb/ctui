package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The review picker lists the submodules checked out in a ticket's worktree. Choosing one runs
// tuicr in it. If you export comments there (y), they go straight to the ticket's Claude session.

// repoInfo is one row of the review picker.
type repoInfo struct {
	Name, Path, Branch string
	Ahead              int // commits on HEAD that aren't on the default branch, -1 if it's unknown
	Dirty              int // files with uncommitted changes
}

// repoStatus reads a submodule's branch and how much it has changed.
func repoStatus(path string) repoInfo {
	ri := repoInfo{Name: filepath.Base(path), Path: path, Ahead: -1}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", path}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	ri.Branch = git("symbolic-ref", "--short", "-q", "HEAD")
	if ri.Branch == "" {
		ri.Branch = "(detached)"
	}
	if st := git("status", "--porcelain"); st != "" {
		ri.Dirty = len(strings.Split(st, "\n"))
	}
	for _, base := range []string{git("rev-parse", "--abbrev-ref", "origin/HEAD"), "origin/main", "origin/master", "main", "master"} {
		if base == "" || git("rev-parse", "--verify", "-q", base) == "" {
			continue
		}
		if n, err := strconv.Atoi(git("rev-list", "--count", base+"..HEAD")); err == nil {
			ri.Ahead = n
		}
		break
	}
	return ri
}

func (ri repoInfo) summary() string {
	var parts []string
	if ri.Ahead > 0 {
		parts = append(parts, plural(ri.Ahead, "commit"))
	}
	if ri.Dirty > 0 {
		parts = append(parts, plural(ri.Dirty, "file")+" dirty")
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}

// tuicrDoneMsg arrives when tuicr exits. out is what it printed: the exported comments, or
// nothing if you quit without exporting.
type tuicrDoneMsg struct {
	ticket string
	repo   repoInfo
	bin    string // the tuicr that ran
	out    string
	err    error
}

func (m *model) startReview(r row) {
	switch {
	case r.WT == nil:
		m.say(false, "%s has no worktree yet", r.Ticket)
		return
	case len(r.WT.Repos) == 0:
		m.say(false, "%s has no submodules checked out", r.Ticket)
		return
	}
	m.reviewTicket = r.Ticket
	m.loadRepos(r)
	m.repoCur = 0
	m.mode = mReview
}

func (m *model) loadRepos(r row) {
	m.repos = nil
	if r.WT == nil {
		return
	}
	for _, sub := range r.WT.Repos {
		m.repos = append(m.repos, repoStatus(filepath.Join(r.WT.Path, sub)))
	}
	m.repoCur = min(m.repoCur, max(0, len(m.repos)-1))
}

// reviewRow is the row of the ticket the picker is for.
func (m *model) reviewRow() (row, bool) {
	for _, r := range m.rows {
		if r.Ticket == m.reviewTicket {
			return r, true
		}
	}
	return row{}, false
}

func (m *model) runTuicr(ri repoInfo) tea.Cmd {
	bin, bundled, err := tuicrPath(m.cfg)
	if err != nil {
		m.say(false, "%v", err)
		return nil
	}
	ticket := m.reviewTicket
	var out bytes.Buffer
	// tuicr draws on the terminal and, with --stdout, prints the exported review on stdout.
	args := []string{"--stdout"}
	if bundled {
		args = append(args, "--no-update-check") // it's updated with ctui, not by itself
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = ri.Path
	cmd.Stdout = &out
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return tuicrDoneMsg{ticket: ticket, repo: ri, bin: bin, out: out.String(), err: err}
	})
}

func (m *model) tuicrDone(msg tuicrDoneMsg) {
	m.refresh(true)
	if r, ok := m.reviewRow(); ok {
		m.loadRepos(r)
	}
	if msg.err != nil {
		m.say(false, "tuicr: %v", msg.err)
		return
	}
	comments := strings.TrimSpace(msg.out)
	if comments == "" {
		m.say(true, "no comments exported from %s; nothing sent", msg.repo.Name)
		return
	}
	r, ok := m.reviewRow()
	switch {
	case !ok || !r.alive():
		m.say(false, "%s has no running Claude; comments kept in tuicr (export them again later)", msg.ticket)
		return
	case r.Status == dWaiting:
		m.say(false, "%s is showing a prompt; comments kept in tuicr (export them again once it's answered)", msg.ticket)
		return
	}
	text := strings.NewReplacer("{repo}", msg.repo.Name, "{repo_path}", msg.repo.Path, "{comments}", comments).
		Replace(m.cfg.Prompt(m.cfg.ReviewPrompt, msg.ticket))
	if err := sendText(r.Pane.ID, text); err != nil {
		m.say(false, "%v (comments kept in tuicr)", err)
		return
	}
	if err := clearTuicrComments(msg.bin, msg.repo.Path); err != nil {
		m.say(false, "sent %s comments to %s, but couldn't clear them in tuicr: %v", msg.repo.Name, msg.ticket, err)
		return
	}
	m.say(true, "sent %s comments to %s", msg.repo.Name, msg.ticket)
}

// clearTuicrComments removes the comments from the repo's most recent tuicr session that has any,
// which is the one just exported, so they aren't sent twice. Reviewed-file marks stay.
func clearTuicrComments(bin, repo string) error {
	out, err := exec.Command(bin, "review", "list", "--repo", repo).Output()
	if err != nil {
		return err
	}
	var sessions []struct {
		Slug     string `json:"slug"`
		Kind     string `json:"kind"`
		Updated  string `json:"updated_at"`
		Comments int    `json:"comment_count"`
	}
	if err := json.Unmarshal(out, &sessions); err != nil {
		return err
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated > sessions[j].Updated })
	for _, s := range sessions {
		if s.Kind == "local" && s.Comments > 0 {
			if out, err := exec.Command(bin, "review", "clearc", "--repo", repo, "--session", s.Slug).CombinedOutput(); err != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	return nil
}

func (m *model) approve(r row) {
	switch {
	case !r.atGate():
		m.say(false, "%s isn't waiting for approval", r.Ticket)
		return
	case !r.alive():
		m.say(false, "%s has no running Claude (enter resumes it)", r.Ticket)
		return
	case r.Status == dWaiting:
		m.say(false, "%s is showing a prompt; open it to answer first", r.Ticket)
		return
	}
	if err := sendText(r.Pane.ID, m.cfg.Prompt(m.cfg.ApprovePrompt, r.Ticket)); err != nil {
		m.say(false, "%v", err)
		return
	}
	m.mode = mNormal
	m.say(true, "approved %s", r.Ticket)
}

func (m *model) updateReview(msg tea.KeyMsg) tea.Cmd {
	r, ok := m.reviewRow()
	if !ok {
		m.mode = mNormal
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		m.mode = mNormal
	case "ctrl+c":
		return tea.Quit
	case "j", "down":
		m.repoCur = min(m.repoCur+1, max(0, len(m.repos)-1))
	case "k", "up":
		m.repoCur = max(m.repoCur-1, 0)
	case "enter":
		if m.repoCur < len(m.repos) {
			return m.runTuicr(m.repos[m.repoCur])
		}
	case "a":
		m.approve(r)
	case "l":
		m.mode = mNormal
		return m.open(r)
	}
	return nil
}
