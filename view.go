package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

var (
	cAccent = lipgloss.Color("141") // soft purple
	cSelBg  = lipgloss.Color("236")

	stWaiting = fg("203").Bold(true)
	stDone    = fg("221").Bold(true)
	stWorking = fg("114")
	stDim     = fg("243")
	stFaint   = fg("239")
	stLabel   = fg("245")
	stBorder  = fg("240")
	stTitle   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stKey     = lipgloss.NewStyle().Foreground(cAccent)
	stErr     = fg("203")
	stOK      = fg("114")
	spinner   = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// panel draws a rounded box exactly w wide and h tall. title sits on the left of the top
// border and right on its right side. Body lines are truncated and padded to fit.
func panel(title, right string, body []string, w, h int) string {
	if w < 10 || h < 2 {
		return ""
	}
	inner := w - 4 // "│ " + content + " │"

	rightPart := ""
	if right != "" {
		rightPart = " " + right + " "
	}
	// ╭─ title ─────── right ─╮
	fill := w - 6 - lipgloss.Width(title) - lipgloss.Width(rightPart)
	if fill < 1 && rightPart != "" {
		rightPart = ""
		fill = w - 6 - lipgloss.Width(title)
	}
	if fill < 1 {
		title = ansi.Truncate(title, max(0, w-7), "…")
		fill = w - 6 - lipgloss.Width(title)
	}
	var b strings.Builder
	b.WriteString(stBorder.Render("╭─ ") + title + stBorder.Render(" "+strings.Repeat("─", max(0, fill))) +
		rightPart + stBorder.Render("─╮") + "\n")

	side := stBorder.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], inner, "…")
		}
		pad := max(0, inner-ansi.StringWidth(line))
		b.WriteString(side + " " + line + "\x1b[0m" + strings.Repeat(" ", pad) + " " + side + "\n")
	}
	b.WriteString(stBorder.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.mode == mHelp {
		return m.helpView()
	}
	w, avail := m.w, m.h-1 // last line is the footer
	inner := w - 4

	// heights: list grows with the number of tickets (up to a third of the screen),
	// details is fixed, and the preview gets the rest
	const detailsH = 5
	listBody := min(max(1, len(m.rows)), max(1, avail/3-2))
	listH := listBody + 2
	previewH := avail - listH - detailsH
	if previewH < 3 {
		previewH = 0
	}

	// scroll the list to keep the cursor visible
	if m.cur < m.offset {
		m.offset = m.cur
	}
	if m.cur >= m.offset+listBody {
		m.offset = m.cur - listBody + 1
	}

	// ── list ──
	var list []string
	if len(m.rows) == 0 {
		list = append(list, stDim.Render("no sessions or worktrees yet. Press ")+stKey.Render("n")+stDim.Render(" to start one."))
	}
	for i := m.offset; i < min(len(m.rows), m.offset+listBody); i++ {
		list = append(list, m.rowLine(m.rows[i], i == m.cur, inner))
	}
	out := []string{panel(stTitle.Render("ctui")+stDim.Render("  "+tildify(m.cfg.Root)), m.counts(), list, w, listH)}

	// ── details ──
	r, ok := m.selected()
	if ok {
		out = append(out, panel(stTitle.Render(r.Ticket), "", m.details(r), w, detailsH))
	} else {
		out = append(out, panel(stDim.Render("details"), "", nil, w, detailsH))
	}

	// ── preview ──
	if previewH > 0 {
		var lines []string
		switch {
		case ok && m.preview != "":
			lines = strings.Split(m.preview, "\n")
			if n := previewH - 2; len(lines) > n {
				lines = lines[len(lines)-n:]
			}
		case ok && r.St.SessionID != "":
			lines = []string{stDim.Render("no Claude running. Press ") + stKey.Render("enter") + stDim.Render(" to resume the conversation.")}
		case ok:
			lines = []string{stDim.Render("no Claude running")}
		}
		out = append(out, panel(stDim.Render("session"), "", lines, w, previewH))
	}

	return strings.Join(out, "\n") + "\n" + m.footer()
}

// rowLine renders a list entry: selection bar, status icon, ticket.
func (m *model) rowLine(r row, sel bool, inner int) string {
	bg := func(s lipgloss.Style) lipgloss.Style {
		if sel {
			return s.Background(cSelBg)
		}
		return s
	}
	icon, ist := m.icon(r.Status)
	bar := " "
	if sel {
		bar = bg(stTitle).Render("▌")
	}
	name := bg(lipgloss.NewStyle().Bold(sel)).Render(r.Ticket)
	line := bar + bg(lipgloss.NewStyle()).Render(" ") + bg(ist).Render(icon) + bg(lipgloss.NewStyle()).Render(" ") + name
	if pad := inner - ansi.StringWidth(line); pad > 0 {
		line += bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", pad))
	}
	return line
}

func (m *model) details(r row) []string {
	label := func(s string) string { return stLabel.Render(fmt.Sprintf("%-9s", s)) }
	blurb := lipgloss.NewStyle().Bold(true).Render(r.St.Blurb)
	if r.St.Blurb == "" {
		blurb = stDim.Italic(true).Render("no blurb yet")
	}
	repos, where := stDim.Render("—"), stDim.Render("—")
	switch {
	case r.WT != nil:
		if len(r.WT.Repos) > 0 {
			repos = strings.Join(r.WT.Repos, stFaint.Render(", "))
		} else {
			repos = stDim.Render("none checked out")
		}
		where = r.WT.Branch + stFaint.Render("  ·  ") + stDim.Render(tildify(r.WT.Path))
	case r.alive():
		where = stDim.Italic(true).Render("setting up…")
	}
	return []string{
		blurb,
		label("repos") + repos,
		label("branch") + where,
	}
}

func (m *model) counts() string {
	c := map[string]int{}
	for _, r := range m.rows {
		c[r.Status]++
	}
	var parts []string
	if n := c[dWaiting]; n > 0 {
		parts = append(parts, stWaiting.Render(fmt.Sprintf("! %d", n)))
	}
	if n := c[dDone]; n > 0 {
		parts = append(parts, stDone.Render(fmt.Sprintf("● %d", n)))
	}
	if n := c[dWorking] + c[dStarting]; n > 0 {
		parts = append(parts, stWorking.Render(fmt.Sprintf("%s %d", spinner[m.frame%len(spinner)], n)))
	}
	return strings.Join(parts, "  ")
}

func (m *model) icon(status string) (string, lipgloss.Style) {
	switch status {
	case dWaiting:
		return "!", stWaiting
	case dDone:
		return "●", stDone
	case dWorking:
		return spinner[m.frame%len(spinner)], stWorking
	case dStarting:
		return spinner[m.frame%len(spinner)], stDim
	case dStale:
		return "◌", stDim
	case dIdle:
		return "○", stLabel
	case dExited:
		return "✗", stDim
	default:
		return "·", stFaint
	}
}

func (m *model) footer() string {
	switch m.mode {
	case mNew, mReply:
		return " " + m.input.View()
	case mConfirm:
		return " " + stDone.Render(m.confirmQ)
	}
	if m.flash != "" && time.Since(m.flashAt) < 4*time.Second {
		if m.flashOK {
			return " " + stOK.Render(m.flash)
		}
		return " " + stErr.Render(m.flash)
	}
	keys := [][2]string{{"j/k", "move"}, {"enter", "open"}, {"n", "new"}, {"i", "reply"}, {"f", "finish"}, {"x", "kill"}, {"?", "help"}, {"q", "quit"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, stKey.Render(k[0])+" "+stDim.Render(k[1]))
	}
	return ansi.Truncate(" "+strings.Join(parts, stFaint.Render("  ·  ")), m.w, "…")
}

func (m *model) helpView() string {
	keys := [][2]string{
		{"j / k", "move"},
		{"g / G", "top / bottom"},
		{"enter, l", "open the session (resumes the conversation if Claude isn't running)"},
		{"n", "new session: type a ticket (5170 or PROJ-5170)"},
		{"i", "send a reply to the selected session without switching to it"},
		{"f", "send the finish prompt (check it's pushed, then finish-feature.sh)"},
		{"x", "kill the Claude window (keeps the worktree)"},
		{"r", "refresh now"},
		{"q", "quit (esc also quits in --popup mode)"},
	}
	icons := [][3]string{
		{"!", "waiting", "asking for permission or asked you a question"},
		{"●", "done", "finished a turn you haven't looked at yet"},
		{"⠋", "working", "Claude is running"},
		{"○", "idle", "waiting for your next prompt"},
		{"◌", "idle?", "hooks say working, but nothing is running on screen (Esc?)"},
		{"·", "no claude", "worktree exists, Claude isn't running"},
	}
	var lines []string
	for _, k := range keys {
		lines = append(lines, stKey.Render(fmt.Sprintf("%-10s", k[0]))+k[1])
	}
	lines = append(lines, "")
	for _, ic := range icons {
		st := stLabel
		switch ic[0] {
		case "!":
			st = stWaiting
		case "●":
			st = stDone
		case "⠋":
			st = stWorking
		case "◌", "·":
			st = stDim
		}
		lines = append(lines, st.Render(ic[0])+"  "+st.Render(fmt.Sprintf("%-10s", ic[1]))+stDim.Render(ic[2]))
	}
	lines = append(lines, "", stDim.Render("press any key"))
	w := min(m.w, 90)
	return panel(stTitle.Render("help"), "", lines, w, min(m.h, len(lines)+2))
}
