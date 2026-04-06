package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
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
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Errorf("Expected nil cmd for 'a'")
	}

	// Test unknown msg
	newM, cmd = m.Update("unknown")
	if cmd != nil {
		t.Errorf("Expected nil cmd for unknown msg")
	}

	// Test LogMsg processing and completion
	msg1 := LogMsg{RepoName: "repo1", Action: "CLONE", Message: "clone ok"}
	newM, cmd = m.Update(msg1)
	m2 := newM.(model)
	if m2.done != 1 || m2.cloned != 1 {
		t.Errorf("Expected done=1, cloned=1, got done=%d cloned=%d", m2.done, m2.cloned)
	}

	msg2 := LogMsg{RepoName: "repo2", Action: "SYNC", Message: "sync ok"}
	newM, cmd = m2.Update(msg2)
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
	if !strings.Contains(view, "Corral") {
		t.Errorf("Expected view to contain Corral")
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
