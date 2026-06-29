// Package tui provides a Bubble Tea terminal user interface for Corral.
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sebastienrousseau/corral/internal/github"
)

// LogMsg represents a log entry to be displayed in the TUI.
type LogMsg struct {
	// RepoName is the name of the repository the entry refers to.
	RepoName string
	// Action is the operation performed (for example CLONE, SYNC, SKIP, ERROR).
	Action string
	// Message is a human-readable description of the outcome.
	Message string
}

// model represents the state of the Bubble Tea application.
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

// NewModel initializes a new TUI model with the expected total number of items.
func NewModel(total int) tea.Model {
	return model{
		total: total,
		prog:  progress.New(progress.WithDefaultGradient()),
	}
}

// Init initializes the Bubble Tea application (no-op).
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
		switch msg.Message {
		case "git clone":
			m.cloned++
		case "git pull":
			m.synced++
		}
	}
}

// Update handles incoming Bubble Tea messages, advancing progress and stats as
// repository results arrive and quitting when the run completes or is cancelled.
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

// View renders the current progress bar, recent log lines, and, once finished,
// the final summary of the run.
func (m model) View() string {
	pad := strings.Repeat(" ", 2)
	percent := 1.0
	if m.total > 0 {
		percent = float64(m.done) / float64(m.total)
	}
	progBar := m.prog.ViewAs(percent)

	var header string
	if os.Getenv("CORRAL_SHOW_LOGO") != "0" {
		header = GetStyledLogo("Organising Repositories")
	} else {
		header = titleStyle.Render("Corral - Organising Repositories") + "\n"
	}

	out := header
	out += pad + progBar + fmt.Sprintf(" %d/%d", m.done, m.total) + "\n\n"

	for _, l := range m.logs {
		icon := "✓"
		if l.Action == "ERROR" || strings.HasPrefix(l.Action, "FAIL") {
			icon = "✗"
		} else if l.Action == "SKIP" {
			icon = "-"
		}
		out += logStyle.Render(fmt.Sprintf("%s [%s] %s: %s", icon, l.Action, l.RepoName, l.Message)) + "\n"
	}

	if m.quitting {
		if m.done >= m.total {
			out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓ Done.")
		} else {
			out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ Aborted.")
		}
		out += fmt.Sprintf(" Cloned %d repos, synced %d repos, kept %d repos, %d failures.\n", m.cloned, m.synced, m.existing, m.failed)
	}

	return out
}

type selectorModel struct {
	repos     []github.Repo
	filter    string
	cursor    int
	selected  map[string]bool // key is repo.Name
	confirmed bool
	quitting  bool
}

func NewSelectorModel(repos []github.Repo) tea.Model {
	sel := make(map[string]bool)
	for _, r := range repos {
		sel[r.Name] = true // select all by default
	}
	return selectorModel{
		repos:    repos,
		selected: sel,
	}
}

func (m selectorModel) Init() tea.Cmd {
	return nil
}

func (m selectorModel) getFiltered() []github.Repo {
	if m.filter == "" {
		return m.repos
	}
	var out []github.Repo
	for _, r := range m.repos {
		if strings.Contains(strings.ToLower(r.Name), strings.ToLower(m.filter)) ||
			strings.Contains(strings.ToLower(r.Language), strings.ToLower(m.filter)) {
			out = append(out, r)
		}
	}
	return out
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	filtered := m.getFiltered()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(filtered)-1 {
				m.cursor++
			}
		case " ", "space":
			if len(filtered) > 0 && m.cursor < len(filtered) {
				name := filtered[m.cursor].Name
				m.selected[name] = !m.selected[name]
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.cursor = 0
			}
		case "a": // Select all
			for _, r := range filtered {
				m.selected[r.Name] = true
			}
		case "n": // Select none / clear all
			for _, r := range filtered {
				m.selected[r.Name] = false
			}
		default:
			// Append to filter if it is a printable character
			if len(msg.String()) == 1 && msg.Runes[0] >= 32 && msg.Runes[0] <= 126 {
				m.filter += msg.String()
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	filtered := m.getFiltered()

	var header string
	if os.Getenv("CORRAL_SHOW_LOGO") != "0" {
		header = GetStyledLogo("Select Repositories")
	} else {
		header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("Corral - Select Repositories") + "\n\n"
	}

	out := header
	out += fmt.Sprintf("  Search/Filter: %s_\n", lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(m.filter))
	out += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("  (Found %d repositories, cursor at %d)", len(filtered), m.cursor+1)) + "\n\n"

	// Display viewport of repositories (limit to 12 items to prevent overflow)
	start := 0
	if m.cursor >= 10 {
		start = m.cursor - 9
	}
	end := start + 12
	if end > len(filtered) {
		end = len(filtered)
	}

	for i := start; i < end; i++ {
		r := filtered[i]
		
		// Checkbox state styling: filled coral circle (●) for checked, empty gray circle (○) for unchecked
		var checked string
		if m.selected[r.Name] {
			checked = lipgloss.NewStyle().Foreground(lipgloss.Color("#F56B5E")).Render("●")
		} else {
			checked = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("○")
		}

		// Repository name styling: highlighted bold white when active
		nameStr := r.Name
		if i == m.cursor {
			nameStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Render(nameStr)
		}

		// Language tag styling: muted gray parenthesized language
		langStr := ""
		if r.Language != "" && r.Language != "Other" {
			langStr = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(" (" + r.Language + ")")
		}

		// Cursor arrow styling: coral triangle pointer (▸) when active
		cursorStr := " "
		if i == m.cursor {
			cursorStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Render("▸")
		}

		out += fmt.Sprintf(" %s %s %s%s\n", cursorStr, checked, nameStr, langStr)
	}

	if len(filtered) == 0 {
		out += "  No repositories match your filter.\n"
	}

	out += "\n"
	out += lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("  [space] toggle • [a] all • [n] none • [enter] confirm • [esc] cancel") + "\n"

	return out
}

func RunSelector(repos []github.Repo) ([]github.Repo, bool) {
	p := tea.NewProgram(NewSelectorModel(repos))
	m, err := p.Run()
	if err != nil {
		return nil, false
	}
	selModel := m.(selectorModel)
	if !selModel.confirmed {
		return nil, false
	}
	var out []github.Repo
	for _, r := range repos {
		if selModel.selected[r.Name] {
			out = append(out, r)
		}
	}
	return out, true
}

var logoLines = []string{
	`         ⡀ ⢠ ⢐⡂ ⡄ ⢀         `,
	`       ⢀⡄⢿ ⢼⡆⣄⢠⢰⡥ ⡿⢠⡀       `,
	`      ⠆⢲⣡⠱⢸⡋⠐⡌⢡⠂⢙⡇⠏⣌⡆⠰      `,
	`     ⠘⠎⡀⠹⢷⡎ ⢭⣹⣏⡥ ⢱⡾⠏⢀⠱⠃     `,
	`    ⠰⠦⠌⣇⣎⣢⣿⡰⢀⢹⡏⡀⢆⣿⣔⣱⣸⠥⠴⠆    `,
	`    ⢠⣐⢶⣦ ⠚⡎⢷⡌⣼⣧⢡⡾⠱⠓ ⣴⡶⣂⡄    `,
	`    ⠈⡼⡵⢽⣌⢤⠉⢤⠻⣼⣧⠏⡤⢉⡤⣡⡯⠮⠥⠁    `,
	`     ⠈⠋⠡⣰⣛⠟⠲⣵⣹⣏⣮⠖⠻⣛⣖⠌⠙⠁     `,
	`           ⠐⠺⣿⣿⠗            `,
	`           ⢀⣠⡿⣷⣄⡀           `,
}

func GetStyledLogo(subtitle string) string {
	colors := []string{
		"#F87171",
		"#FA5B4E",
		"#F25447",
		"#E14F44",
		"#D5473D",
		"#C93F36",
		"#BD362E",
		"#B02E28",
		"#A22030",
		"#9F1239",
	}
	var sb strings.Builder
	sb.WriteString("\n")
	for i, line := range logoLines {
		sb.WriteString("     " + lipgloss.NewStyle().Foreground(lipgloss.Color(colors[i])).Render(line) + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F56B5E")).Render("  ⧇ CORRAL") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("  •  "+subtitle) + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("  "+strings.Repeat("─", 38)) + "\n\n")
	return sb.String()
}
