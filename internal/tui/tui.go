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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}
	case LogMsg:
		m.done++
		m.logs = append(m.logs, msg)
		if len(m.logs) > 10 {
			m.logs = m.logs[1:]
		}
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
	if m.quitting && m.done >= m.total {
		return "Done.\n"
	}
	if m.quitting {
		return "Aborted.\n"
	}

	pad := strings.Repeat(" ", 2)
	percent := float64(m.done) / float64(m.total)
	progBar := m.prog.ViewAs(percent)

	out := titleStyle.Render("Corral - Organising Repositories") + "\n"
	out += pad + progBar + fmt.Sprintf(" %d/%d", m.done, m.total) + "\n\n"

	for _, l := range m.logs {
		out += logStyle.Render(fmt.Sprintf("[%s] %s: %s", l.Action, l.RepoName, l.Message)) + "\n"
	}

	return out
}
