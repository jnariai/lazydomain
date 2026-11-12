package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jnariai/lazydomain/internal/doctor"
	"github.com/jnariai/lazydomain/internal/sites"
)

var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5A4FCF", Dark: "#8B83FF"}
	colOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	colErr    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}

	panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colMuted).Padding(0, 1)
	title = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	muted = lipgloss.NewStyle().Foreground(colMuted)
	okS   = lipgloss.NewStyle().Foreground(colOK)
	warnS = lipgloss.NewStyle().Foreground(colWarn)
	errS  = lipgloss.NewStyle().Foreground(colErr)
	selS  = lipgloss.NewStyle().Bold(true).Reverse(true)
	keyS  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
)

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	header := m.headerLine()
	footer := m.footerLines()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 5 {
		bodyH = 5
	}

	leftW := m.width / 3
	if leftW < 24 {
		leftW = 24
	}
	if leftW > 40 {
		leftW = 40
	}
	rightW := m.width - leftW

	// Each panel's border and padding take 4 columns and 2 rows.
	left := panel.Width(leftW - 2).Height(bodyH - 2).Render(clip(m.listView(leftW-4), bodyH-2))
	right := panel.Width(rightW - 2).Height(bodyH - 2).Render(clip(m.rightView(rightW-4), bodyH-2))

	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinHorizontal(lipgloss.Top, left, right), footer)
}

func (m model) headerLine() string {
	proxy := okS.Render("proxy ● up")
	if m.snap.ProxyErr != nil {
		proxy = errS.Render("proxy ● down (u to start)")
	}
	root := m.snap.Root
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(root, home) {
		root = "~" + strings.TrimPrefix(root, home)
	}
	return " " + title.Render("lazydomain") + muted.Render("  ·  ") + proxy + muted.Render("  ·  "+root)
}

func (m model) footerLines() string {
	var status string
	switch {
	case m.busy != "":
		status = m.spinner.View() + " " + m.busy
	case m.confirm != nil:
		status = warnS.Render(fmt.Sprintf("Remove https://%s? Press y to confirm, any other key to cancel.", m.confirm.Domain()))
	case m.flashErr:
		status = errS.Render("✗ " + m.flash)
	case m.flash != "":
		status = okS.Render(m.flash)
	}

	keys := []string{"↑↓ move", "a add", "x remove", "o open", "r refresh", "D doctor", "u proxy up", "t trust", "q quit"}
	for i, k := range keys {
		parts := strings.SplitN(k, " ", 2)
		keys[i] = keyS.Render(parts[0]) + " " + muted.Render(parts[1])
	}
	help := " " + strings.Join(keys, muted.Render("  "))
	line := lipgloss.NewStyle().Width(m.width)
	return line.Render(" "+status) + "\n" + line.Render(help)
}

func (m model) listView(w int) string {
	var b strings.Builder
	b.WriteString(title.Render("Projects") + "\n\n")
	if !m.loaded {
		return b.String() + muted.Render("scanning…")
	}
	if m.loadErr != nil {
		return b.String() + errS.Render(m.loadErr.Error())
	}
	if len(m.snap.Entries) == 0 {
		return b.String() + muted.Render("No Laravel projects in "+m.snap.Root)
	}
	for i, e := range m.snap.Entries {
		dot, style := entryDot(e)
		name := truncate(e.Name, w-3)
		line := style.Render(dot) + " " + name
		if i == m.cursor {
			line = selS.Render(dot + " " + padRight(name, w-2))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func entryDot(e sites.Entry) (string, lipgloss.Style) {
	switch {
	case e.Kind == sites.Managed && e.Status.OK():
		return "●", okS
	case e.Kind == sites.Managed:
		return "●", warnS
	case e.Kind == sites.Available:
		return "○", lipgloss.NewStyle().Foreground(colAccent)
	default:
		return "○", muted
	}
}

func (m model) rightView(w int) string {
	if m.view == viewDoctor {
		return m.doctorView(w)
	}
	e, ok := m.selected()
	if !ok {
		return muted.Render("Nothing selected.")
	}

	var b strings.Builder
	b.WriteString(title.Render(e.Name) + "\n\n")

	appState := errS.Render("down")
	if e.AppUp {
		appState = okS.Render("up")
	}
	version := "unknown"
	if e.Project != nil && e.Project.Version != "" {
		version = e.Project.Version
	}

	switch e.Kind {
	case sites.Managed:
		s := e.Site
		fmt.Fprintf(&b, "%s  → 127.0.0.1:%d  %s\n", padRight("https://"+s.Domain(), 34), s.AppPort, appState)
		fmt.Fprintf(&b, "%s  → 127.0.0.1:%d  %s\n\n", padRight("https://"+s.ViteDomain(), 34), s.VitePort, muted.Render("vite"))
		if e.Status.OK() {
			b.WriteString(okS.Render("✓ active") + muted.Render("  proxy route, hosts entry and ports match") + "\n")
		} else {
			for _, p := range e.Status.Problems {
				b.WriteString(warnS.Render("! "+p) + "\n")
			}
			b.WriteString(muted.Render("press a to re-apply") + "\n")
		}
	case sites.Available:
		if e.PlanErr != nil {
			b.WriteString(warnS.Render("Can't add yet:") + "\n" + wrap(e.PlanErr.Error(), w) + "\n")
		} else {
			fmt.Fprintf(&b, "Not served. Press %s to serve it at %s\n", keyS.Render("a"), title.Render("https://"+e.Domain()))
			fmt.Fprintf(&b, "%s\n", muted.Render(fmt.Sprintf("app → 127.0.0.1:%d (%s), vite → 127.0.0.1:%d", e.AppPort, stripANSI(appState), e.Plan.Site.VitePort)))
			for _, warn := range e.Plan.Warnings {
				b.WriteString(warnS.Render(wrap("! "+warn, w)) + "\n")
			}
		}
	default:
		b.WriteString(muted.Render(e.Reason) + "\n")
	}

	fmt.Fprintf(&b, "\n%s %s  %s %s\n", muted.Render("Laravel"), version, muted.Render("Path"), tildify(e.Dir))

	if steps := m.nextSteps(e); steps != "" {
		b.WriteString("\n" + steps)
	}
	return b.String()
}

func (m model) nextSteps(e sites.Entry) string {
	if e.Project == nil {
		return ""
	}
	var steps []sites.Step
	switch {
	case e.Kind == sites.Managed:
		steps = sites.NextSteps(e.Project, *e.Site)
	case e.Kind == sites.Available && e.Plan != nil:
		steps = sites.NextSteps(e.Project, e.Plan.Site)
	default:
		return ""
	}

	var b strings.Builder
	b.WriteString(title.Render("Project setup") + muted.Render("  (lazydomain never edits your project)") + "\n\n")
	if len(steps) == 0 {
		b.WriteString(okS.Render("✓ APP_URL, trustProxies and Vite are configured") + "\n")
		return b.String()
	}
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Title)
		for _, line := range strings.Split(s.Body, "\n") {
			b.WriteString("   " + muted.Render(line) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(muted.Render("Then restart: sail down && sail up -d, and sail npm run dev") + "\n")
	return b.String()
}

func (m model) doctorView(w int) string {
	var b strings.Builder
	b.WriteString(title.Render("Doctor") + muted.Render("  (D or esc to close)") + "\n\n")
	if m.doctorBusy {
		return b.String() + m.spinner.View() + " running checks…"
	}
	for _, r := range m.doctor {
		var sym string
		switch r.Status {
		case doctor.OK:
			sym = okS.Render("✓")
		case doctor.Warn:
			sym = warnS.Render("!")
		case doctor.Fail:
			sym = errS.Render("✗")
		default:
			sym = muted.Render("-")
		}
		fmt.Fprintf(&b, "%s %s %s\n", sym, padRight(r.Name, 21), truncate(r.Detail, w-24))
		if r.Hint != "" && (r.Status == doctor.Warn || r.Status == doctor.Fail) {
			for _, line := range strings.Split(r.Hint, "\n") {
				b.WriteString("    " + muted.Render(line) + "\n")
			}
		}
	}
	return b.String()
}

func clip(s string, h int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > h {
		lines = append(lines[:h-1], muted.Render("…"))
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, w int) string {
	if w <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

func padRight(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func wrap(s string, w int) string { return lipgloss.NewStyle().Width(w).Render(s) }

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func tildify(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
