import React from 'react';
import { Box, Text } from 'ink';
import type { Stats } from '../types.js';

interface HeaderProps {
  connected: boolean;
  stats: Stats;
  isPaused: boolean;
}

export function Header({ connected, stats, isPaused }: HeaderProps): React.ReactElement {
  const connectionStatus = connected ? (
    <Text color="green">Connected</Text>
  ) : (
    <Text color="red">Disconnected</Text>
  );

  const pauseStatus = isPaused ? (
    <Text color="yellow"> [PAUSED]</Text>
  ) : null;

  return (
    <Box
      borderStyle="single"
      borderColor="blue"
      paddingX={1}
      flexDirection="row"
      justifyContent="space-between"
    >
      {/* Title */}
      <Box>
        <Text bold color="cyan">
          Canopy Dashboard
        </Text>
        {pauseStatus}
      </Box>

      {/* Stats */}
      <Box gap={2}>
        <Text>
          <Text color="green">{stats.completed_tasks}</Text>
          <Text dimColor>/</Text>
          <Text>{stats.total_tasks}</Text>
          <Text dimColor> tasks</Text>
        </Text>
        <Text>
          <Text color="blue">{stats.running_tasks}</Text>
          <Text dimColor> running</Text>
        </Text>
        {stats.failed_tasks > 0 && (
          <Text>
            <Text color="red">{stats.failed_tasks}</Text>
            <Text dimColor> failed</Text>
          </Text>
        )}
        <Text>
          <Text color="yellow">${stats.total_cost_usd.toFixed(2)}</Text>
        </Text>
      </Box>

      {/* Connection Status */}
      <Box>
        {connectionStatus}
      </Box>
    </Box>
  );
}
