package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jzila/canopy/pkg/daemon"
)

func TestNewDashboard(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	dashboard := NewDashboard(state, eventBus)

	if dashboard == nil {
		t.Fatal("NewDashboard returned nil")
	}

	if dashboard.state != state {
		t.Error("Dashboard state not set correctly")
	}

	if dashboard.eventBus != eventBus {
		t.Error("Dashboard eventBus not set correctly")
	}
}

func TestDashboardInit(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	dashboard := NewDashboard(state, eventBus)
	cmd := dashboard.Init()

	if cmd == nil {
		t.Error("Init should return a command")
	}

	// Verify unsubscribe was set
	if dashboard.unsubscribe == nil {
		t.Error("Init should subscribe to event bus")
	}
}

func TestDashboardUpdate(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	dashboard := NewDashboard(state, eventBus)
	dashboard.Init()

	// Test window size message
	msg := tea.WindowSizeMsg{Width: 100, Height: 40}
	model, _ := dashboard.Update(msg)
	d := model.(*Dashboard)

	if d.width != 100 {
		t.Errorf("Expected width 100, got %d", d.width)
	}
	if d.height != 40 {
		t.Errorf("Expected height 40, got %d", d.height)
	}
	if !d.ready {
		t.Error("Dashboard should be ready after window size message")
	}
}

func TestDashboardRefreshAgents(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	// Add some agents
	agent1 := &daemon.AgentState{
		ID:        "agent-1",
		TaskID:    "task-1",
		TaskTitle: "Test Task 1",
		Status:    daemon.AgentStatusRunning,
		StartTime: time.Now(),
	}
	agent2 := &daemon.AgentState{
		ID:        "agent-2",
		TaskID:    "task-2",
		TaskTitle: "Test Task 2",
		Status:    daemon.AgentStatusCompleted,
		StartTime: time.Now().Add(-time.Minute),
	}
	state.AddAgent(agent1)
	state.AddAgent(agent2)

	dashboard := NewDashboard(state, eventBus)
	dashboard.refreshAgents()

	if len(dashboard.agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(dashboard.agents))
	}

	// Running agents should be sorted first
	if dashboard.agents[0].Status != daemon.AgentStatusRunning {
		t.Error("Running agents should be sorted first")
	}
}

func TestDashboardView(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	dashboard := NewDashboard(state, eventBus)

	// Before ready
	view := dashboard.View()
	if view != "Initializing dashboard..." {
		t.Errorf("Expected initializing message, got: %s", view)
	}

	// After ready
	dashboard.ready = true
	dashboard.width = 100
	dashboard.height = 40
	view = dashboard.View()

	if len(view) == 0 {
		t.Error("View should return non-empty string")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{3661 * time.Second, "1h1m"},
	}

	for _, tc := range tests {
		result := formatDuration(tc.duration)
		if result != tc.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", tc.duration, result, tc.expected)
		}
	}
}

func TestDashboardQuit(t *testing.T) {
	state := daemon.NewRuntimeState()
	eventBus := daemon.NewEventBus()

	dashboard := NewDashboard(state, eventBus)
	dashboard.Init()
	dashboard.ready = true
	dashboard.width = 100
	dashboard.height = 40

	// Simulate quit
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	model, cmd := dashboard.Update(msg)
	d := model.(*Dashboard)

	if !d.quitting {
		t.Error("Dashboard should be quitting after 'q' key")
	}

	// cmd should be tea.Quit
	if cmd == nil {
		t.Error("Quit command should be returned")
	}
}
