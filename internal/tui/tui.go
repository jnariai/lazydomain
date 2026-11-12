package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jnariai/lazydomain/internal/doctor"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/sites"
	"github.com/jnariai/lazydomain/internal/state"
)

const refreshEvery = 5 * time.Second

func Run(ctx context.Context) error {
	m := newModel(ctx)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

type view int

const (
	viewDetails view = iota
	viewDoctor
)

type model struct {
	ctx  context.Context
	plat platform.Info

	snap    sites.Snapshot
	loadErr error
	loaded  bool
	cursor  int

	view       view
	doctor     []doctor.Result
	doctorBusy bool

	busy    string
	spinner spinner.Model
	confirm *sites.Entry

	flash    string
	flashErr bool

	width, height int
}

func newModel(ctx context.Context) model {
	return model{
		ctx:     ctx,
		plat:    platform.Detect(),
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
}

type snapshotMsg struct {
	snap sites.Snapshot
	err  error
}

type tickMsg struct{}

type actionDoneMsg struct {
	text string
	err  error
}

type doctorMsg []doctor.Result

func (m model) refresh() tea.Cmd {
	return func() tea.Msg {
		st, err := state.Load()
		if err != nil {
			return snapshotMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		return snapshotMsg{snap: sites.Collect(ctx, m.plat, st)}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) addCmd(e sites.Entry) tea.Cmd {
	return func() tea.Msg {
		st, err := state.Load()
		if err != nil {
			return actionDoneMsg{err: err}
		}
		plan, err := sites.PlanAdd(st, e.Dir)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		if err := sites.Apply(m.ctx, m.plat, st, plan.Next); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{text: fmt.Sprintf("✓ added https://%s", plan.Site.Domain())}
	}
}

func (m model) removeCmd(e sites.Entry) tea.Cmd {
	return func() tea.Msg {
		st, err := state.Load()
		if err != nil {
			return actionDoneMsg{err: err}
		}
		site, ok := st.ByDir(e.Dir)
		if !ok {
			return actionDoneMsg{err: fmt.Errorf("%s is not managed anymore", e.Name)}
		}
		if err := sites.Apply(m.ctx, m.plat, st, sites.PlanRemove(st, site)); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{text: fmt.Sprintf("✓ removed https://%s", site.Domain())}
	}
}

func (m model) openCmd(url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		if err := platform.OpenURL(ctx, m.plat, url); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{text: "opened " + url}
	}
}

func (m model) doctorCmd() tea.Cmd {
	return func() tea.Msg { return doctorMsg(doctor.Run(m.ctx)) }
}

func execSelf(label string, args ...string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return actionDoneMsg{err: err} }
	}
	const wrapped = `"$0" "$@"; status=$?; printf '\nPress Enter to return to lazydomain…'; read _; exit $status`
	cmd := exec.Command("sh", append([]string{"-c", wrapped, self}, args...)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("%s failed: %w", label, err)}
		}
		return actionDoneMsg{text: "✓ " + label + " finished"}
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick(), m.spinner.Tick)
}

func (m model) selected() (sites.Entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.snap.Entries) {
		return sites.Entry{}, false
	}
	return m.snap.Entries[m.cursor], true
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case snapshotMsg:
		m.loaded = true
		m.loadErr = msg.err
		if msg.err == nil {
			m.snap = msg.snap
			if m.cursor >= len(m.snap.Entries) {
				m.cursor = max(0, len(m.snap.Entries)-1)
			}
		}
		return m, nil

	case tickMsg:
		if m.busy != "" {
			return m, tick()
		}
		return m, tea.Batch(m.refresh(), tick())

	case actionDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.flash, m.flashErr = msg.err.Error(), true
		} else {
			m.flash, m.flashErr = msg.text, false
		}
		return m, m.refresh()

	case doctorMsg:
		m.doctor, m.doctorBusy = msg, false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.busy != "" {
		return m, nil
	}

	if m.confirm != nil {
		e := *m.confirm
		m.confirm = nil
		if key == "y" || key == "Y" {
			m.busy = "Removing https://" + e.Domain() + " (accept the UAC prompt if Windows shows one)"
			return m, m.removeCmd(e)
		}
		m.flash, m.flashErr = "remove cancelled", false
		return m, nil
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.snap.Entries)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(m.snap.Entries)-1)
	case "r":
		m.flash = ""
		return m, m.refresh()
	case "esc":
		m.view, m.flash = viewDetails, ""
	case "D":
		if m.view == viewDoctor {
			m.view = viewDetails
			return m, nil
		}
		m.view, m.doctor, m.doctorBusy = viewDoctor, nil, true
		return m, m.doctorCmd()
	case "u":
		return m, execSelf("proxy up", "proxy", "up")
	case "t":
		return m, execSelf("trust", "trust")
	case "a":
		e, ok := m.selected()
		switch {
		case !ok:
		case e.Kind == sites.Managed:
			m.busy = "Re-applying https://" + e.Domain() + " (accept the UAC prompt if Windows shows one)"
			return m, m.addCmd(e)
		case e.Kind == sites.Available && e.PlanErr == nil:
			m.busy = "Adding https://" + e.Domain() + " (accept the UAC prompt if Windows shows one)"
			return m, m.addCmd(e)
		case e.Kind == sites.Available:
			m.flash, m.flashErr = e.PlanErr.Error(), true
		default:
			m.flash, m.flashErr = e.Name+" is "+e.Reason, true
		}
	case "x", "d", "delete":
		if e, ok := m.selected(); ok && e.Kind == sites.Managed {
			m.confirm = &e
		}
	case "o":
		if e, ok := m.selected(); ok && e.Kind == sites.Managed {
			return m, m.openCmd("https://" + e.Domain())
		}
	}
	return m, nil
}
