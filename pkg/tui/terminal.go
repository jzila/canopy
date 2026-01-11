package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jzila/canopy/pkg/daemon"
)

// TerminalModel represents the terminal pane that displays agent output
type TerminalModel struct {
	agent    *daemon.AgentState
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// NewTerminalModel creates a new terminal model
func NewTerminalModel() TerminalModel {
	return TerminalModel{
		viewport: viewport.New(0, 0),
	}
}

// Init initializes the terminal model
func (m TerminalModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the terminal model
func (m TerminalModel) Update(msg tea.Msg) (TerminalModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Update dimensions
		headerHeight := 3 // border + header line + border
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width-2, msg.Height-headerHeight)
			m.viewport.KeyMap.Up.SetKeys("k")
			m.viewport.KeyMap.Down.SetKeys("j")
			m.viewport.KeyMap.HalfPageUp.SetKeys("ctrl+u")
			m.viewport.KeyMap.HalfPageDown.SetKeys("ctrl+d")
			m.viewport.KeyMap.PageUp.SetKeys("ctrl+b")
			m.viewport.KeyMap.PageDown.SetKeys("ctrl+f")
			m.ready = true
		} else {
			m.viewport.Width = msg.Width - 2
			m.viewport.Height = msg.Height - headerHeight
		}

		m.updateViewportContent()

	case tea.KeyMsg:
		switch msg.String() {
		case "g":
			// Go to top
			m.viewport.GotoTop()
		case "G":
			// Go to bottom
			m.viewport.GotoBottom()
		default:
			// Let viewport handle other keys
			m.viewport, cmd = m.viewport.Update(msg)
		}

	case AgentSelectedMsg:
		// Agent selection changed
		m.agent = msg.Agent
		m.updateViewportContent()

	case AgentOutputMsg:
		// New output for current agent
		if m.agent != nil && msg.AgentID == m.agent.ID {
			m.updateViewportContent()
			// Auto-scroll to bottom if we're near the bottom
			if m.viewport.AtBottom() || m.viewport.ScrollPercent() > 0.9 {
				m.viewport.GotoBottom()
			}
		}
	}

	return m, cmd
}

// View renders the terminal pane
func (m TerminalModel) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Styles
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	selectedBorderStyle := borderStyle.Copy().
		BorderForeground(lipgloss.Color("86"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	if m.agent == nil {
		emptyContent := dimStyle.Render("Select an agent to view output")
		return borderStyle.Width(m.width - 2).Height(m.height - 2).Render(emptyContent)
	}

	// Build header
	agentID := m.agent.ID
	if len(agentID) > 8 {
		agentID = agentID[:8]
	}
	title := m.agent.TaskTitle
	if len(title) > 40 {
		title = title[:40] + "..."
	}

	header := headerStyle.Render("Output: "+agentID) + "  " + dimStyle.Render(title)

	// Build scroll indicator
	scrollInfo := ""
	if m.viewport.TotalLineCount() > 0 {
		percent := int(m.viewport.ScrollPercent() * 100)
		scrollInfo = dimStyle.Render(fmt.Sprintf("%d%%", percent))
	}

	headerLine := lipgloss.JoinHorizontal(
		lipgloss.Top,
		header,
		strings.Repeat(" ", max(0, m.width-lipgloss.Width(header)-lipgloss.Width(scrollInfo)-4)),
		scrollInfo,
	)

	// Combine header and viewport
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerLine,
		m.viewport.View(),
	)

	return selectedBorderStyle.
		Width(m.width - 2).
		Render(content)
}

// updateViewportContent refreshes the viewport content based on current agent state
func (m *TerminalModel) updateViewportContent() {
	if m.agent == nil {
		m.viewport.SetContent("")
		return
	}

	var sections []string

	// Render styled live feed events first (the main content)
	if len(m.agent.LiveFeedEvents) > 0 {
		liveFeedContent := RenderLiveFeedEvents(m.agent.LiveFeedEvents, m.viewport.Width)
		if liveFeedContent != "" {
			sections = append(sections, liveFeedContent)
		}
	}

	// Get raw stdout/stderr
	stdout, stderr := m.agent.Output.Get()

	// Style for stderr (red)
	stderrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Add stdout lines if present
	if stdout != "" {
		// Add a separator and header if we have live feed events
		if len(sections) > 0 {
			separator := dimStyle.Render(strings.Repeat("═", min(m.viewport.Width, 50)))
			header := dimStyle.Render("─── Raw Output ───")
			sections = append(sections, separator, header)
		}

		stdoutLines := strings.Split(stdout, "\n")
		var outputLines []string
		for _, line := range stdoutLines {
			outputLines = append(outputLines, line)
		}
		if len(outputLines) > 0 {
			sections = append(sections, strings.Join(outputLines, "\n"))
		}
	}

	// Add stderr lines with red color
	if stderr != "" {
		stderrLines := strings.Split(stderr, "\n")
		var errorLines []string
		for _, line := range stderrLines {
			if line != "" {
				errorLines = append(errorLines, stderrStyle.Render(line))
			}
		}
		if len(errorLines) > 0 {
			// Add error header if we have other content
			if len(sections) > 0 {
				errorHeader := stderrStyle.Render("─── Errors ───")
				sections = append(sections, errorHeader)
			}
			sections = append(sections, strings.Join(errorLines, "\n"))
		}
	}

	// Set viewport content
	content := strings.Join(sections, "\n")
	m.viewport.SetContent(content)
}

// SetAgent updates the selected agent
func (m *TerminalModel) SetAgent(agent *daemon.AgentState) {
	m.agent = agent
	m.updateViewportContent()
}

// AgentSelectedMsg is sent when an agent is selected
type AgentSelectedMsg struct {
	Agent *daemon.AgentState
}

// AgentOutputMsg is sent when agent output is updated
type AgentOutputMsg struct {
	AgentID string
	Output  string
	IsError bool
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
