package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/events"
)

// Dashboard is the main TUI model that displays agent status, output, and progress
type Dashboard struct {
	state         *daemon.RuntimeState
	eventBus      *events.EventBus
	unsubscribe   func()
	agents        []*daemon.AgentState // sorted list of agents
	selectedIndex int
	terminal      TerminalModel
	spinner       spinner.Model
	width         int
	height        int
	ready         bool
	quitting      bool
}

// NewDashboard creates a new dashboard TUI with in-process state and event bus
func NewDashboard(state *daemon.RuntimeState, eventBus *events.EventBus) *Dashboard {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	return &Dashboard{
		state:    state,
		eventBus: eventBus,
		terminal: NewTerminalModel(),
		spinner:  s,
	}
}

// NewRemoteDashboard creates a dashboard that connects to a remote daemon
// via WebSocket. The addr can be a port number, host:port, or full URL.
func NewRemoteDashboard(addr string) (*Dashboard, *RemoteClient, error) {
	client := NewRemoteClient(addr)

	if err := client.Connect(); err != nil {
		return nil, nil, fmt.Errorf("failed to connect to daemon at %s: %w", addr, err)
	}

	dashboard := NewDashboard(client.GetState(), client.GetEventBus())
	return dashboard, client, nil
}

// RunRemote starts a dashboard that connects to a remote daemon
func RunRemote(addr string) error {
	dashboard, client, err := NewRemoteDashboard(addr)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	return dashboard.Run()
}

// Init initializes the dashboard
func (d *Dashboard) Init() tea.Cmd {
	// Subscribe to event bus for real-time updates
	if d.eventBus != nil {
		d.unsubscribe = d.eventBus.Subscribe(func(event events.Event) {
			// Events are processed via polling (tickMsg)
		})
	}

	return tea.Batch(
		d.spinner.Tick,
		tickCmd(),
	)
}

// tickMsg triggers periodic state refresh
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages and updates the dashboard state
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		d.ready = true

		// Update terminal size (right pane gets ~60% width)
		termWidth := msg.Width * 6 / 10
		termHeight := msg.Height - 6 // leave room for header and stats
		termMsg := tea.WindowSizeMsg{Width: termWidth, Height: termHeight}
		d.terminal, _ = d.terminal.Update(termMsg)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			d.quitting = true
			if d.unsubscribe != nil {
				d.unsubscribe()
			}
			return d, tea.Quit
		case "j", "down":
			if len(d.agents) > 0 {
				d.selectedIndex = (d.selectedIndex + 1) % len(d.agents)
				d.updateSelectedAgent()
			}
		case "k", "up":
			if len(d.agents) > 0 {
				d.selectedIndex = (d.selectedIndex - 1 + len(d.agents)) % len(d.agents)
				d.updateSelectedAgent()
			}
		case "g":
			// Go to first agent
			if len(d.agents) > 0 {
				d.selectedIndex = 0
				d.updateSelectedAgent()
			}
		case "G":
			// Go to last agent
			if len(d.agents) > 0 {
				d.selectedIndex = len(d.agents) - 1
				d.updateSelectedAgent()
			}
		default:
			// Pass key to terminal for scrolling
			d.terminal, _ = d.terminal.Update(msg)
		}

	case tickMsg:
		// Refresh state and agent list
		d.refreshAgents()
		// Update terminal with latest output if agent selected
		if d.selectedIndex < len(d.agents) {
			agent := d.agents[d.selectedIndex]
			d.terminal, _ = d.terminal.Update(AgentOutputMsg{
				AgentID: agent.ID,
				Output:  "",
			})
		}
		cmds = append(cmds, tickCmd())

	case spinner.TickMsg:
		var cmd tea.Cmd
		d.spinner, cmd = d.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return d, tea.Batch(cmds...)
}

// refreshAgents updates the sorted agent list from state
func (d *Dashboard) refreshAgents() {
	if d.state == nil {
		return
	}

	snapshot := d.state.GetSnapshot()
	d.agents = make([]*daemon.AgentState, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		// Use GetSnapshot to avoid copying the mutex
		agentCopy := agent.GetSnapshot()
		d.agents = append(d.agents, &agentCopy)
	}

	// Sort by start time (newest first for running, completed at end)
	sort.Slice(d.agents, func(i, j int) bool {
		// Running agents first
		iRunning := d.agents[i].Status == daemon.AgentStatusRunning
		jRunning := d.agents[j].Status == daemon.AgentStatusRunning
		if iRunning != jRunning {
			return iRunning
		}
		// Then by start time (most recent first)
		return d.agents[i].StartTime.After(d.agents[j].StartTime)
	})

	// Ensure selected index is valid
	if d.selectedIndex >= len(d.agents) {
		d.selectedIndex = max(0, len(d.agents)-1)
	}

	// Update selected agent in terminal
	d.updateSelectedAgent()
}

func (d *Dashboard) updateSelectedAgent() {
	if len(d.agents) == 0 {
		d.terminal.SetAgent(nil)
		return
	}
	d.terminal.SetAgent(d.agents[d.selectedIndex])
}

// View renders the dashboard
func (d *Dashboard) View() string {
	if d.quitting {
		return ""
	}

	if !d.ready {
		return "Initializing dashboard..."
	}

	// Get current stats
	var stats daemon.Stats
	if d.state != nil {
		snapshot := d.state.GetSnapshot()
		stats = snapshot.Stats
	}

	// Styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Padding(0, 1)

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Header
	header := headerStyle.Render("🌲 Canopy Dashboard")

	// Stats bar
	statsBar := d.renderStatsBar(stats)

	// Calculate pane dimensions
	leftWidth := d.width * 4 / 10
	rightWidth := d.width - leftWidth - 1 // -1 for separator

	// Left pane: Agent list
	leftPane := d.renderAgentList(leftWidth, d.height-4)

	// Right pane: Terminal output
	termMsg := tea.WindowSizeMsg{Width: rightWidth, Height: d.height - 4}
	d.terminal, _ = d.terminal.Update(termMsg)
	rightPane := d.terminal.View()

	// Combine panes
	panes := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftPane,
		rightPane,
	)

	// Help bar
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)
	help := helpStyle.Render("j/k: select • g/G: first/last • q: quit • scroll: j/k/ctrl+u/ctrl+d")

	// Combine all
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		statsStyle.Render(statsBar),
		panes,
		help,
	)

	return content
}

// renderStatsBar renders the statistics bar
func (d *Dashboard) renderStatsBar(stats daemon.Stats) string {
	running := lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render(fmt.Sprintf("▶ %d", stats.RunningTasks))
	completed := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(fmt.Sprintf("✓ %d", stats.CompletedTasks))
	failed := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("✗ %d", stats.FailedTasks))

	// Format tokens with input/output breakdown
	tokens := fmt.Sprintf("in: %dk out: %dk", stats.TotalInputTokens/1000, stats.TotalOutputTokens/1000)

	// Add cache info if significant
	cacheInfo := ""
	if stats.TotalCacheReadTokens > 0 {
		cacheInfo = fmt.Sprintf(" (cache: %dk)", stats.TotalCacheReadTokens/1000)
	}

	cost := fmt.Sprintf("$%.2f", stats.TotalCostUSD)
	turns := fmt.Sprintf("turns: %d", stats.TotalTurns)

	return fmt.Sprintf("  %s  %s  %s  │  %s%s  │  %s  │  %s", running, completed, failed, tokens, cacheInfo, turns, cost)
}

// renderAgentList renders the left pane agent list
func (d *Dashboard) renderAgentList(width, height int) string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(width - 2).
		Height(height - 2)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Foreground(lipgloss.Color("255"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	if len(d.agents) == 0 {
		content := dimStyle.Render("Waiting for agents...")
		return borderStyle.Render(headerStyle.Render("Agents") + "\n\n" + content)
	}

	var lines []string
	lines = append(lines, headerStyle.Render("Agents"))
	lines = append(lines, "")

	for i, agent := range d.agents {
		// Status indicator
		var status string
		switch agent.Status {
		case daemon.AgentStatusRunning:
			status = d.spinner.View()
		case daemon.AgentStatusCompleted:
			status = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
		case daemon.AgentStatusFailed:
			status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
		default:
			status = dimStyle.Render("○")
		}

		// Agent ID (truncated)
		agentID := agent.ID
		if len(agentID) > 8 {
			agentID = agentID[:8]
		}

		// Task title (truncated)
		title := agent.TaskTitle
		maxTitleLen := width - 16
		if maxTitleLen < 10 {
			maxTitleLen = 10
		}
		if len(title) > maxTitleLen {
			title = title[:maxTitleLen-3] + "..."
		}

		// Duration
		var duration string
		if agent.Status == daemon.AgentStatusRunning {
			elapsed := time.Since(agent.StartTime)
			duration = formatDuration(elapsed)
		} else if agent.Duration > 0 {
			duration = formatDuration(time.Duration(agent.Duration * float64(time.Second)))
		}

		// Turns info
		var turnsInfo string
		if agent.NumTurns > 0 {
			turnsInfo = fmt.Sprintf("%dt", agent.NumTurns)
		}

		// Git commits info
		var commitsInfo string
		commitCount := len(agent.GitCommits)
		if commitCount == 0 && agent.Commits > 0 {
			commitCount = agent.Commits // fallback to legacy count
		}
		if commitCount > 0 {
			commitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // purple for commits
			commitsInfo = commitStyle.Render(fmt.Sprintf("⎇%d", commitCount))
		}

		// Merge status indicator
		var mergeInfo string
		if agent.MergeStatus != "" && agent.MergeStatus != daemon.MergeStatusNone {
			mergeStyle := lipgloss.NewStyle()
			switch agent.MergeStatus {
			case daemon.MergeStatusPending:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("240")) // dim
				mergeInfo = mergeStyle.Render("⋯q")
			case daemon.MergeStatusAcquiring:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("214")) // orange
				mergeInfo = mergeStyle.Render("⤻")
			case daemon.MergeStatusMerging:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("33")) // blue
				mergeInfo = mergeStyle.Render("⤵")
			case daemon.MergeStatusResolving:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("214")) // orange
				mergeInfo = mergeStyle.Render("⚡")
			case daemon.MergeStatusMerged:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("42")) // green
				mergeInfo = mergeStyle.Render("⤴")
			case daemon.MergeStatusFailed:
				mergeStyle = mergeStyle.Foreground(lipgloss.Color("196")) // red
				mergeInfo = mergeStyle.Render("⤫")
			}
		}

		line := fmt.Sprintf("%s %s %s", status, agentID, title)
		if duration != "" {
			line += dimStyle.Render(" " + duration)
		}
		if turnsInfo != "" {
			line += dimStyle.Render(" " + turnsInfo)
		}
		if commitsInfo != "" {
			line += " " + commitsInfo
		}
		if mergeInfo != "" {
			line += " " + mergeInfo
		}

		// Apply selection style
		if i == d.selectedIndex {
			// Pad line to full width for selection highlight
			padLen := width - 4 - lipgloss.Width(line)
			if padLen > 0 {
				line += strings.Repeat(" ", padLen)
			}
			line = selectedStyle.Render(line)
		}

		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return borderStyle.Render(content)
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// Run starts the dashboard TUI
func (d *Dashboard) Run() error {
	p := tea.NewProgram(d, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
