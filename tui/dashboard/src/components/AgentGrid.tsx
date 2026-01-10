import React from 'react';
import { Box, Text } from 'ink';
import type { AgentState, AgentStatus } from '../types.js';

interface AgentGridProps {
  agents: Record<string, AgentState>;
  selectedAgentId: string | null;
  onSelectAgent: (id: string | null) => void;
}

// Status colors and symbols
const statusDisplay: Record<AgentStatus, { color: string; symbol: string }> = {
  starting: { color: 'yellow', symbol: '...' },
  running: { color: 'blue', symbol: '>>>' },
  completed: { color: 'green', symbol: '[+]' },
  failed: { color: 'red', symbol: '[X]' },
  timed_out: { color: 'yellow', symbol: '[T]' },
  cancelled: { color: 'gray', symbol: '[-]' },
};

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const mins = Math.floor(seconds / 60);
  const secs = Math.round(seconds % 60);
  return `${mins}m${secs}s`;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`;
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(1)}K`;
  return String(tokens);
}

interface AgentCardProps {
  agent: AgentState;
  isSelected: boolean;
}

function AgentCard({ agent, isSelected }: AgentCardProps): React.ReactElement {
  const status = statusDisplay[agent.status] || statusDisplay.running;
  const borderColor = isSelected ? 'cyan' : 'gray';

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderColor={borderColor}
      paddingX={1}
      width={28}
    >
      {/* Agent ID and Status */}
      <Box justifyContent="space-between">
        <Text bold color={isSelected ? 'cyan' : 'white'}>
          {agent.id.slice(0, 8)}
        </Text>
        <Text color={status.color}>{status.symbol}</Text>
      </Box>

      {/* Task Title */}
      <Text wrap="truncate-end" dimColor>
        {agent.task_title.slice(0, 24)}
      </Text>

      {/* Stats Row */}
      <Box justifyContent="space-between">
        <Text dimColor>{formatDuration(agent.duration)}</Text>
        <Text dimColor>{formatTokens(agent.token_usage.total_tokens)} tok</Text>
        <Text color="yellow">${agent.token_usage.cost_usd.toFixed(2)}</Text>
      </Box>
    </Box>
  );
}

export function AgentGrid({
  agents,
  selectedAgentId,
}: AgentGridProps): React.ReactElement {
  const agentList = Object.values(agents).sort((a, b) => {
    // Running agents first, then by start time
    if (a.status === 'running' && b.status !== 'running') return -1;
    if (a.status !== 'running' && b.status === 'running') return 1;
    return new Date(b.start_time).getTime() - new Date(a.start_time).getTime();
  });

  if (agentList.length === 0) {
    return (
      <Box
        flexDirection="column"
        flexGrow={1}
        alignItems="center"
        justifyContent="center"
        borderStyle="single"
        borderColor="gray"
      >
        <Text dimColor>No agents running</Text>
        <Text dimColor>Waiting for tasks...</Text>
      </Box>
    );
  }

  return (
    <Box
      flexDirection="row"
      flexWrap="wrap"
      flexGrow={1}
      gap={1}
      borderStyle="single"
      borderColor="gray"
      padding={1}
    >
      {agentList.map((agent) => (
        <AgentCard
          key={agent.id}
          agent={agent}
          isSelected={agent.id === selectedAgentId}
        />
      ))}
    </Box>
  );
}
