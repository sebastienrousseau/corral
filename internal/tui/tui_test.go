package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebastienrousseau/corral/internal/github"
)

func TestModelInit(t *testing.T) {
	m := NewModel(10)
	if m.Init() != nil {
		t.Errorf("Expected Init to return nil")
	}
}

func TestModelUpdate(t *testing.T) {
	m := NewModel(2).(model)

	// Test KeyMsg quit (q)
	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil || !newM.(model).quitting {
		t.Errorf("Expected quitting on 'q'")
	}

	// Test KeyMsg quit (ctrl+c)
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || !newM.(model).quitting {
		t.Errorf("Expected quitting on 'ctrl+c'")
	}

	// Test KeyMsg not quit
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Errorf("Expected nil cmd for 'a'")
	}

	// Test unknown msg
	_, cmd = m.Update("unknown")
	if cmd != nil {
		t.Errorf("Expected nil cmd for unknown msg")
	}

	// Test LogMsg processing and completion
	msg1 := LogMsg{RepoName: "repo1", Action: "CLONE", Message: "clone ok"}
	newM, _ = m.Update(msg1)
	m2 := newM.(model)
	if m2.done != 1 || m2.cloned != 1 {
		t.Errorf("Expected done=1, cloned=1, got done=%d cloned=%d", m2.done, m2.cloned)
	}

	msg2 := LogMsg{RepoName: "repo2", Action: "SYNC", Message: "sync ok"}
	newM, _ = m2.Update(msg2)
	m3 := newM.(model)
	if !m3.quitting || m3.done != 2 || m3.synced != 1 {
		t.Errorf("Expected quitting=true, done=2, synced=1, got quitting=%v done=%d synced=%d", m3.quitting, m3.done, m3.synced)
	}

	// Test progress.FrameMsg
	frameMsg := progress.FrameMsg{}
	_, _ = m3.Update(frameMsg)
}

func TestProcessLogMsg(t *testing.T) {
	m := NewModel(10).(model)

	m.processLogMsg(LogMsg{Action: "CLONE"})
	m.processLogMsg(LogMsg{Action: "SYNC"})
	m.processLogMsg(LogMsg{Action: "ERROR"})
	m.processLogMsg(LogMsg{Action: "SKIP"})
	m.processLogMsg(LogMsg{Action: "DRY-RUN", Message: "git clone"})
	m.processLogMsg(LogMsg{Action: "DRY-RUN", Message: "git pull"})

	// Fill logs over 10 to trigger shift
	for i := 0; i < 15; i++ {
		m.processLogMsg(LogMsg{Action: "CLONE"})
	}

	if len(m.logs) != 10 {
		t.Errorf("Expected 10 logs, got %d", len(m.logs))
	}
	if m.cloned != 17 { // 1 clone + 1 dry-run + 15 loop
		t.Errorf("Expected 17 cloned, got %d", m.cloned)
	}
	if m.synced != 2 {
		t.Errorf("Expected 2 synced, got %d", m.synced)
	}
	if m.failed != 1 {
		t.Errorf("Expected 1 failed, got %d", m.failed)
	}
	if m.existing != 1 {
		t.Errorf("Expected 1 existing, got %d", m.existing)
	}
}

func TestModelView(t *testing.T) {
	m := NewModel(10).(model)

	m.processLogMsg(LogMsg{RepoName: "repo1", Action: "CLONE", Message: "ok"})
	m.processLogMsg(LogMsg{RepoName: "repo2", Action: "ERROR", Message: "fail"})
	m.processLogMsg(LogMsg{RepoName: "repo3", Action: "FAIL", Message: "fail"})
	m.processLogMsg(LogMsg{RepoName: "repo4", Action: "SKIP", Message: "skip"})

	view := m.View()
	if !strings.Contains(strings.ToLower(view), "perfect sync") && !strings.Contains(strings.ToLower(view), "organising repositories") {
		t.Errorf("Expected view to contain branding text")
	}

	m.quitting = true
	viewAborted := m.View()
	if !strings.Contains(viewAborted, "Aborted.") {
		t.Errorf("Expected view to contain Aborted.")
	}

	m.done = 10
	viewDone := m.View()
	if !strings.Contains(viewDone, "Done.") {
		t.Errorf("Expected view to contain Done.")
	}
}

func TestSelectorModel(t *testing.T) {
	repos := []github.Repo{
		{Name: "repo1", Language: "Go"},
		{Name: "repo2", Language: "Rust"},
	}

	fetchFn := func() ([]github.Repo, error) {
		return repos, nil
	}
	m := NewSelectorModel(fetchFn)
	newM, _ := m.Update(fetchedReposMsg{repos: repos})
	m = newM.(*selectorModel)

	if m.Init() == nil {
		t.Errorf("Expected selector Init to return non-nil loading commands")
	}

	// Test viewport filtering
	if len(m.filteredRepos) != 2 {
		t.Errorf("Expected 2 repos, got %d", len(m.filteredRepos))
	}

	// Test typing/filtering
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m2 := newM.(*selectorModel)
	if m2.filter != "g" {
		t.Errorf("Expected filter to be 'g', got %q", m2.filter)
	}
	if len(m2.filteredRepos) != 1 || m2.filteredRepos[0].Name != "repo1" {
		t.Errorf("Expected only repo1 to match filter 'g', got %v", m2.filteredRepos)
	}

	// Test backspace
	newM, _ = m2.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m3 := newM.(*selectorModel)
	if m3.filter != "" {
		t.Errorf("Expected empty filter, got %q", m3.filter)
	}

	// Test navigation (down/up)
	newM, _ = m3.Update(tea.KeyMsg{Type: tea.KeyDown})
	m4 := newM.(*selectorModel)
	if m4.table.Cursor() != 1 {
		t.Errorf("Expected cursor at 1 after down key, got %d", m4.table.Cursor())
	}

	newM, _ = m4.Update(tea.KeyMsg{Type: tea.KeyUp})
	m5 := newM.(*selectorModel)
	if m5.table.Cursor() != 0 {
		t.Errorf("Expected cursor at 0 after up key, got %d", m5.table.Cursor())
	}

	// Test toggle selection (initially true, toggled to false)
	newM, _ = m5.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m6 := newM.(*selectorModel)
	if m6.selected["repo1"] != false {
		t.Errorf("Expected repo1 selected to toggle to false")
	}

	// Test select none ('ctrl+n')
	newM, _ = m6.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m7 := newM.(*selectorModel)
	if m7.selected["repo1"] != false {
		t.Errorf("Expected repo1 selected to toggle to false after 'ctrl+n'")
	}

	// Test select all ('ctrl+a')
	newM, _ = m7.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m8 := newM.(*selectorModel)
	if m8.selected["repo1"] != true || m8.selected["repo2"] != true {
		t.Errorf("Expected both to be selected after 'ctrl+a'")
	}

	// Test cancel
	newM, _ = m8.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m9 := newM.(*selectorModel)
	if !m9.quitting {
		t.Errorf("Expected quitting to be true after Esc")
	}

	// Test confirm
	newM, _ = m8.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m10 := newM.(*selectorModel)
	if !m10.confirmed {
		t.Errorf("Expected confirmed to be true after Enter")
	}

	// Test render View
	view := m10.View()
	if !strings.Contains(view, "Corral.") && !strings.Contains(view, "Search repositories") {
		t.Errorf("expected view to contain header elements, got %s", view)
	}
}

func TestSlashCommands(t *testing.T) {
	repos := []github.Repo{
		{Name: "c-repo", Language: "Python", Visibility: "public"},
		{Name: "a-repo", Language: "Go", Visibility: "private"},
		{Name: "b-repo", Language: "Rust", Visibility: "public"},
	}
	m := NewSelectorModel(func() ([]github.Repo, error) {
		return repos, nil
	})
	
	newM, _ := m.Update(fetchedReposMsg{repos: repos, err: nil})
	model := newM.(*selectorModel)
	
	// Test /all
	model.executeSlashCommand("/all")
	if !model.selected["a-repo"] || !model.selected["b-repo"] || !model.selected["c-repo"] {
		t.Errorf("Expected all repos to be selected")
	}

	// Test /none
	model.executeSlashCommand("/none")
	if model.selected["a-repo"] || model.selected["b-repo"] || model.selected["c-repo"] {
		t.Errorf("Expected all repos to be deselected")
	}

	// Test /sort name
	model.executeSlashCommand("/sort name")
	if model.filteredRepos[0].Name != "a-repo" || model.filteredRepos[1].Name != "b-repo" || model.filteredRepos[2].Name != "c-repo" {
		t.Errorf("Expected sorted by name: a-repo, b-repo, c-repo; got order: %s, %s, %s", model.filteredRepos[0].Name, model.filteredRepos[1].Name, model.filteredRepos[2].Name)
	}

	// Test /sort language
	model.executeSlashCommand("/sort language")
	if model.filteredRepos[0].Name != "a-repo" || model.filteredRepos[1].Name != "c-repo" || model.filteredRepos[2].Name != "b-repo" {
		t.Errorf("Expected sorted by language: a-repo (Go), c-repo (Python), b-repo (Rust)")
	}

	// Test unknown command
	model.executeSlashCommand("/invalid")
	if !strings.Contains(model.cmdErr, "Unknown command") {
		t.Errorf("Expected unknown command error, got %q", model.cmdErr)
	}
	// Test /exit returns tea.Quit command
	cmdExit := model.executeSlashCommand("/exit")
	if cmdExit == nil {
		t.Errorf("Expected /exit to return a non-nil command")
	}
	// Test character-by-character typing simulation of "/none" and "/all"
	// 1. Reset all to true
	for i := range model.repos {
		model.selected[model.repos[i].Name] = true
	}
	// 2. Type '/'
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if len(model.filteredRepos) != 3 {
		t.Errorf("Expected filteredRepos not to be wiped on slash prefix, got %d", len(model.filteredRepos))
	}
	// 3. Type 'n', 'o', 'n', 'e'
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if model.filter != "/none" {
		t.Errorf("Expected filter to be '/none', got %q", model.filter)
	}
	// 4. Press Enter
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.selected["a-repo"] || model.selected["b-repo"] || model.selected["c-repo"] {
		t.Errorf("Expected all repos to be deselected after typed /none and Enter")
	}
}

