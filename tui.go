package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Display statuses (derived from the stored State plus tmux).
const (
	dWaiting  = "waiting"
	dDone     = "done"
	dWorking  = "working"
	dStarting = "starting"
	dStale    = "idle?" // hooks say working, but the screen has no "esc to interrupt" (e.g. you pressed Esc)
	dIdle     = "idle"
	dExited   = "exited"
	dNone     = "no claude"
)

var prio = map[string]int{dWaiting: 0, dDone: 1, dWorking: 2, dStarting: 2, dStale: 3, dIdle: 4, dExited: 5, dNone: 6}

const staleAfter = 20 * time.Second

type row struct {
	Ticket string
	Status string
	St     State
	Pane   *Pane
	WT     *Worktree
	Type   string    // work type: from `ctui type`, or "review" from the worktree's git config
	Work   *WorkType // nil if the type isn't known
}

func (r row) alive() bool { return r.Pane != nil && !r.Pane.Dead }

// hasStage reports whether the row knows which of its type's stages it's at.
// A review tree detected from git config has a type but no stage.
func (r row) hasStage() bool {
	return r.Work != nil && r.St.Type != "" && r.St.Stage >= 0 && r.St.Stage < len(r.Work.Stages)
}

// atGate reports whether the work is waiting for your approval of a stage.
func (r row) atGate() bool { return r.hasStage() && r.Work.Stages[r.St.Stage] == approveStage }

type mode int

const (
	mNormal mode = iota
	mNew
	mReply
	mConfirm
	mHelp
	mReview // the review picker
)

type tickMsg time.Time

type model struct {
	cfg   Config
	popup bool

	rows   []row
	cur    int
	offset int
	wts    map[string]Worktree
	lastWT time.Time
	lastRf time.Time
	now    time.Time
	frame  int  // spinner frame, from the clock
	pulse  bool // a row is at an approval gate, so tick fast for a smooth pulse

	preview string

	mode         mode
	reviewTicket string
	repos        []repoInfo
	repoCur      int

	input     textinput.Model
	confirmQ  string
	confirmFn func() tea.Cmd

	flash   string
	flashOK bool
	flashAt time.Time

	w, h int
}

func runTUI(cfg Config, popup bool) error {
	ti := textinput.New()
	ti.CharLimit = 4000
	if inTmux() {
		// tmux converts 24-bit colour for terminals that can't show it, so the pulse can
		// always use it. lipgloss can't tell what's behind tmux.
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	m := &model{cfg: cfg, popup: popup, input: ti, now: time.Now()}
	m.refresh(true)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) tick() tea.Cmd {
	d := 120 * time.Millisecond
	if m.pulse {
		d = time.Second / 30
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd { return m.tick() }

// ---------- data ----------

func (m *model) refresh(worktreesToo bool) {
	if worktreesToo || time.Since(m.lastWT) > 5*time.Second {
		m.wts = listWorktrees(m.cfg)
		m.lastWT = time.Now()
	}
	panes := listPanes()
	seen := map[string]bool{}
	for t := range panes {
		seen[t] = true
	}
	for t := range m.wts {
		seen[t] = true
	}

	var selected string
	if m.cur < len(m.rows) {
		selected = m.rows[m.cur].Ticket
	}

	rows := make([]row, 0, len(seen))
	for t := range seen {
		r := row{Ticket: t, St: readState(t)}
		if p, ok := panes[t]; ok {
			r.Pane = &p
		}
		if wt, ok := m.wts[t]; ok {
			r.WT = &wt
		}
		r.Type = r.St.Type
		if r.Type == "" && r.WT != nil && r.WT.Review {
			r.Type = "review"
		}
		if wt, ok := m.cfg.workType(r.Type); ok {
			r.Work = &wt
		}
		r.Status = m.displayStatus(&r)
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if pi, pj := prio[rows[i].Status], prio[rows[j].Status]; pi != pj {
			return pi < pj
		}
		return rows[i].Ticket < rows[j].Ticket
	})
	m.rows = rows
	m.pulse = false
	for _, r := range rows {
		if r.atGate() {
			m.pulse = true
		}
	}
	m.lastRf = time.Now()
	m.cur = 0
	for i, r := range rows {
		if r.Ticket == selected {
			m.cur = i
		}
	}
	m.capturePreview()
}

func (m *model) displayStatus(r *row) string {
	if !r.alive() {
		return dNone
	}
	// Looking at the window counts as having read it.
	if r.St.Unread && r.Pane.Visible() {
		r.St.Unread = false
		_, _ = updateState(r.Ticket, func(s *State) { s.Unread = false })
	}
	// No hook fires when you answer a permission prompt or press Esc, so the screen breaks ties.
	// Claude's status line shows "esc to interrupt" only while it's running.
	running := func() bool { return strings.Contains(strings.ToLower(capturePlain(r.Pane.ID)), "esc to interrupt") }
	switch r.St.State {
	case StWaiting:
		if time.Since(r.St.Updated) > 2*time.Second && running() {
			r.St.Msg = r.St.Tool
			return dWorking
		}
		return dWaiting
	case StWorking:
		if time.Since(r.St.Updated) > staleAfter && !running() {
			return dStale
		}
		return dWorking
	case StIdle:
		if r.St.Unread {
			return dDone
		}
		return dIdle
	case StExited:
		return dExited
	default:
		return dStarting
	}
}

func (m *model) capturePreview() {
	m.preview = ""
	if r, ok := m.selected(); ok && r.alive() {
		m.preview = capturePane(r.Pane.ID)
	}
}

func (m *model) selected() (row, bool) {
	if m.cur >= 0 && m.cur < len(m.rows) {
		return m.rows[m.cur], true
	}
	return row{}, false
}

func (m *model) selectTicket(t string) {
	for i, r := range m.rows {
		if r.Ticket == t {
			m.cur = i
		}
	}
	m.capturePreview()
}

func (m *model) say(ok bool, format string, a ...any) {
	m.flash, m.flashOK, m.flashAt = fmt.Sprintf(format, a...), ok, time.Now()
}

// ---------- actions ----------

func (m *model) open(r row) tea.Cmd {
	if !r.alive() {
		if r.St.SessionID == "" {
			m.say(false, "no recorded Claude conversation for %s (press n to start one)", r.Ticket)
			return nil
		}
		p, err := launch(m.cfg, r.Ticket, "", r.St.SessionID)
		if err != nil {
			m.say(false, "%v", err)
			return nil
		}
		r.Pane = &p
	}
	_, _ = updateState(r.Ticket, func(s *State) { s.Unread = false })
	if err := switchTo(r.Pane.Window); err != nil {
		m.say(false, "%v", err)
		return nil
	}
	if m.popup {
		return tea.Quit
	}
	m.refresh(false)
	return nil
}

func (m *model) newSession(input string) {
	t, err := m.cfg.NormalizeTicket(input)
	if err != nil {
		m.say(false, "%v", err)
		return
	}
	for _, r := range m.rows {
		if r.Ticket == t && r.alive() {
			m.selectTicket(t)
			m.say(false, "%s is already running", t)
			return
		}
	}
	st := readState(t)
	_, wtExists := m.wts[t]
	if wtExists && st.SessionID != "" {
		_, err = launch(m.cfg, t, "", st.SessionID)
		if err == nil {
			m.say(true, "resumed %s", t)
		}
	} else {
		_, err = launch(m.cfg, t, m.cfg.Prompt(m.cfg.StartPrompt, t), "")
		if err == nil {
			m.say(true, "started %s (enter to open)", t)
		}
	}
	if err != nil {
		m.say(false, "%v", err)
		return
	}
	m.refresh(true)
	m.selectTicket(t)
}

func (m *model) finish(r row) {
	prompt := m.cfg.Prompt(m.cfg.FinishPrompt, r.Ticket)
	if r.alive() {
		if err := sendText(r.Pane.ID, prompt); err != nil {
			m.say(false, "%v", err)
			return
		}
		m.say(true, "sent finish prompt to %s", r.Ticket)
		return
	}
	if r.St.SessionID == "" {
		m.say(false, "no recorded Claude conversation for %s", r.Ticket)
		return
	}
	if _, err := launch(m.cfg, r.Ticket, prompt, r.St.SessionID); err != nil {
		m.say(false, "%v", err)
		return
	}
	m.say(true, "resumed %s with the finish prompt", r.Ticket)
	m.refresh(false)
}

func (m *model) reply(r row, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if err := sendText(r.Pane.ID, text); err != nil {
		m.say(false, "%v", err)
		return
	}
	m.say(true, "sent to %s", r.Ticket)
}

func (m *model) kill(r row) {
	if _, err := tmux("kill-pane", "-t", r.Pane.ID); err != nil {
		m.say(false, "%v", err)
		return
	}
	_, _ = updateState(r.Ticket, func(s *State) { s.State = StExited })
	m.say(true, "killed Claude for %s (worktree kept)", r.Ticket)
	m.refresh(false)
}

// ---------- update ----------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.Width = max(10, m.w-20)
		return m, nil
	case tickMsg:
		m.now = time.Time(msg)
		m.frame = int(m.now.UnixMilli() / 100)
		if m.now.Sub(m.lastRf) >= time.Second {
			m.refresh(false)
		}
		return m, m.tick()
	case tuicrDoneMsg:
		m.tuicrDone(msg)
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case mNew, mReply:
			return m, m.updateInput(msg)
		case mConfirm:
			m.mode = mNormal
			if msg.String() == "y" || msg.String() == "Y" {
				return m, m.confirmFn()
			}
			m.say(false, "cancelled")
			return m, nil
		case mHelp:
			m.mode = mNormal
			return m, nil
		case mReview:
			return m, m.updateReview(msg)
		}
		return m, m.updateNormal(msg)
	}
	return m, nil
}

func (m *model) updateInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = mNormal
		m.input.Blur()
		return nil
	case tea.KeyEnter:
		val := m.input.Value()
		md := m.mode
		m.mode = mNormal
		m.input.Blur()
		if md == mNew {
			m.newSession(val)
		} else if r, ok := m.selected(); ok && r.alive() {
			m.reply(r, val)
		}
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *model) startInput(md mode, prompt string) tea.Cmd {
	m.mode = md
	m.input.Prompt = prompt
	m.input.SetValue("")
	return m.input.Focus()
}

func (m *model) confirm(q string, fn func() tea.Cmd) {
	m.mode, m.confirmQ, m.confirmFn = mConfirm, q, fn
}

func (m *model) updateNormal(msg tea.KeyMsg) tea.Cmd {
	r, ok := m.selected()
	moved := false
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "esc":
		if m.popup {
			return tea.Quit
		}
	case "j", "down":
		if m.cur < len(m.rows)-1 {
			m.cur++
			moved = true
		}
	case "k", "up":
		if m.cur > 0 {
			m.cur--
			moved = true
		}
	case "g", "home":
		m.cur, moved = 0, true
	case "G", "end":
		m.cur, moved = max(0, len(m.rows)-1), true
	case "r":
		m.refresh(true)
		m.say(true, "refreshed")
	case "?":
		m.mode = mHelp
	case "n":
		return m.startInput(mNew, "new ticket: ")
	case "enter", "l":
		if ok {
			return m.open(r)
		}
	case "i":
		switch {
		case !ok:
		case !r.alive():
			m.say(false, "%s has no running Claude (enter resumes it)", r.Ticket)
		case r.Status == dWaiting:
			m.say(false, "%s is showing a prompt; open it to answer", r.Ticket)
		default:
			return m.startInput(mReply, r.Ticket+" ❯ ")
		}
	case "f":
		switch {
		case !ok:
		case r.Status == dWaiting:
			m.say(false, "%s is showing a prompt; open it to answer first", r.Ticket)
		default:
			m.confirm(fmt.Sprintf("Send the finish prompt to %s? [y/N]", r.Ticket), func() tea.Cmd { m.finish(r); return nil })
		}
	case "d":
		if ok {
			m.startReview(r)
		}
	case "x":
		if ok && r.alive() {
			m.confirm(fmt.Sprintf("Kill Claude for %s? The worktree is kept. [y/N]", r.Ticket), func() tea.Cmd { m.kill(r); return nil })
		}
	}
	if moved {
		m.capturePreview()
	}
	return nil
}

// ---------- helpers ----------

func tildify(p string) string {
	h := home()
	if rel, err := filepath.Rel(h, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
}
