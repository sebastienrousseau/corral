package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type LogMsg struct {
	RepoName string
	Action   string
	Message  string
}

type model struct {
	total    int
	done     int
	logs     []LogMsg
	prog     progress.Model
	quitting bool

	cloned   int
	synced   int
	failed   int
	existing int
}

func NewModel(total int) model {
	return model{
		total: total,
		prog:  progress.New(progress.WithDefaultGradient()),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) processLogMsg(msg LogMsg) {
	m.done++
	m.logs = append(m.logs, msg)
	if len(m.logs) > 10 {
		m.logs = m.logs[1:]
	}

	switch msg.Action {
	case "CLONE":
		m.cloned++
	case "SYNC":
		m.synced++
	case "ERROR":
		m.failed++
	case "SKIP":
		m.existing++
	case "DRY-RUN":
		if msg.Message == "git clone" {
			m.cloned++
		} else if msg.Message == "git pull" {
			m.synced++
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}
	case LogMsg:
		m.processLogMsg(msg)
		if m.done >= m.total {
			m.quitting = true
			return m, tea.Sequence(m.prog.SetPercent(1.0), tea.Quit)
		}
		return m, m.prog.SetPercent(float64(m.done) / float64(m.total))
	case progress.FrameMsg:
		progressModel, cmd := m.prog.Update(msg)
		m.prog = progressModel.(progress.Model)
		return m, cmd
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	logStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func (m model) View() string {
	pad := strings.Repeat(" ", 2)
	percent := float64(m.done) / float64(m.total)
	progBar := m.prog.ViewAs(percent)

	out := titleStyle.Render("Corral - Organising Repositories") + "\n"
	out += pad + progBar + fmt.Sprintf(" %d/%d", m.done, m.total) + "\n\n"

	for _, l := range m.logs {
		out += logStyle.Render(fmt.Sprintf("[%s] %s: %s", l.Action, l.RepoName, l.Message)) + "\n"
	}

	if m.quitting {
		if m.done >= m.total {
			out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Done.")
		} else {
			out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Aborted.")
		}
		out += fmt.Sprintf(" Cloned %d repos, synced %d repos, kept %d repos, %d failures.\n", m.cloned, m.synced, m.existing, m.failed)
	}

	return out
}
