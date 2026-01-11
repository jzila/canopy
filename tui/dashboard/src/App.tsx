import React, { useState } from 'react';
import { Box, Text, useApp, useInput, useStdout } from 'ink';
import { Header } from './components/Header.js';
import { Sidebar } from './components/Sidebar.js';
import { AgentGrid } from './components/AgentGrid.js';
import { AgentTerminal } from './components/AgentTerminal.js';
import type { Stats, AgentState, TaskState } from './types.js';

// Initial empty stats
const initialStats: Stats = {
  total_tasks: 0,
  completed_tasks: 0,
  failed_tasks: 0,
  running_tasks: 0,
  total_tokens: 0,
  total_cost_usd: 0,
  total_duration: 0,
  avg_duration: 0,
  file_changes: 0,
  git_commits: 0,
};

export interface AppProps {
  // These will be provided by the state store when connected
  connected?: boolean;
  agents?: Record<string, AgentState>;
  tasks?: Record<string, TaskState>;
  stats?: Stats;
  isPaused?: boolean;
}

export function App({
  connected = false,
  agents = {},
  tasks = {},
  stats = initialStats,
  isPaused = false,
}: AppProps): React.ReactElement {
  const { exit } = useApp();
  const { stdout } = useStdout();
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  // Get terminal dimensions for responsive layout
  const terminalWidth = stdout?.columns || 120;
  const terminalHeight = stdout?.rows || 40;

  // Calculate layout heights
  const headerHeight = 3;
  const terminalPanelHeight = 15;
  const mainAreaHeight = terminalHeight - headerHeight - terminalPanelHeight - 2;

  // Handle keyboard input
  useInput((input, key) => {
    // Quit on q or Ctrl+C
    if (input === 'q' || (key.ctrl && input === 'c')) {
      exit();
      return;
    }

    // Navigate agents with j/k or arrow keys
    const agentIds = Object.keys(agents);
    if (agentIds.length > 0) {
      const currentIndex = selectedAgentId
        ? agentIds.indexOf(selectedAgentId)
        : -1;

      if (input === 'j' || key.downArrow) {
        const nextIndex = (currentIndex + 1) % agentIds.length;
        setSelectedAgentId(agentIds[nextIndex]!);
      } else if (input === 'k' || key.upArrow) {
        const prevIndex =
          currentIndex <= 0 ? agentIds.length - 1 : currentIndex - 1;
        setSelectedAgentId(agentIds[prevIndex]!);
      }
    }

    // Tab to switch focus areas (future enhancement)
    if (key.tab) {
      // Toggle between agent grid and task list focus
    }
  });

  // Get selected agent for terminal panel
  const selectedAgent = selectedAgentId ? agents[selectedAgentId] || null : null;

  return (
    <Box
      flexDirection="column"
      width={terminalWidth}
      height={terminalHeight}
    >
      {/* Header */}
      <Header
        connected={connected}
        stats={stats}
        isPaused={isPaused}
      />

      {/* Main Content Area */}
      <Box
        flexDirection="row"
        flexGrow={1}
        height={mainAreaHeight}
      >
        {/* Left Sidebar - Task List */}
        <Sidebar
          tasks={tasks}
          selectedTaskId={selectedTaskId}
        />

        {/* Main Area - Agent Grid */}
        <AgentGrid
          agents={agents}
          selectedAgentId={selectedAgentId}
          onSelectAgent={setSelectedAgentId}
        />
      </Box>

      {/* Bottom Panel - Terminal Output */}
      {selectedAgent ? (
        <AgentTerminal
          agentId={selectedAgent.id}
          taskTitle={selectedAgent.task_title}
          output={selectedAgent.output}
          height={terminalPanelHeight}
          isFocused={false}
        />
      ) : (
        <Box
          flexDirection="column"
          height={terminalPanelHeight}
          borderStyle="single"
          borderColor="gray"
          paddingX={1}
        >
          <Text dimColor>Select an agent to view output (j/k or arrow keys)</Text>
        </Box>
      )}
    </Box>
  );
}
