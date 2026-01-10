import React from 'react';
import { Box, Text } from 'ink';
import type { AgentState } from '../types.js';

interface TerminalPanelProps {
  agent: AgentState | null;
}

export function TerminalPanel({ agent }: TerminalPanelProps): React.ReactElement {
  if (!agent) {
    return (
      <Box
        flexDirection="column"
        height={10}
        borderStyle="single"
        borderColor="gray"
        paddingX={1}
      >
        <Text dimColor>Select an agent to view output</Text>
      </Box>
    );
  }

  // Combine stdout and stderr, keeping last N lines
  const output = agent.output.stdout + agent.output.stderr;
  const lines = output.split('\n').slice(-8); // Keep last 8 lines

  return (
    <Box
      flexDirection="column"
      height={10}
      borderStyle="single"
      borderColor="cyan"
      paddingX={1}
    >
      {/* Header */}
      <Box justifyContent="space-between" borderBottom>
        <Text bold color="cyan">
          Output: {agent.id.slice(0, 8)}
        </Text>
        <Text dimColor>{agent.task_title.slice(0, 40)}</Text>
      </Box>

      {/* Output Content */}
      <Box flexDirection="column" flexGrow={1} overflowY="hidden">
        {lines.length === 0 ? (
          <Text dimColor>No output yet...</Text>
        ) : (
          lines.map((line, i) => (
            <Text key={i} wrap="truncate-end">
              {line}
            </Text>
          ))
        )}
      </Box>
    </Box>
  );
}
