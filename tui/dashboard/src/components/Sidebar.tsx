import React from 'react';
import { Box, Text } from 'ink';
import type { TaskState } from '../types.js';

interface SidebarProps {
  tasks: Record<string, TaskState>;
  selectedTaskId: string | null;
}

// Priority colors
const priorityColors: Record<number, string> = {
  0: 'red',     // P0 - Critical
  1: 'yellow',  // P1 - High
  2: 'blue',    // P2 - Medium
  3: 'cyan',    // P3 - Low
  4: 'white',   // P4 - Backlog
};

// Status colors
const statusColors: Record<string, string> = {
  ready: 'cyan',
  in_progress: 'blue',
  running: 'blue',
  completed: 'green',
  failed: 'red',
  blocked: 'yellow',
};

export function Sidebar({ tasks, selectedTaskId }: SidebarProps): React.ReactElement {
  const taskList = Object.values(tasks).sort((a, b) => {
    // Sort by priority first, then by status
    if (a.priority !== b.priority) return a.priority - b.priority;
    return a.status.localeCompare(b.status);
  });

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderColor="gray"
      width={30}
      height="100%"
    >
      <Box paddingX={1} borderBottom>
        <Text bold>Tasks</Text>
        <Text dimColor> ({taskList.length})</Text>
      </Box>

      <Box flexDirection="column" paddingX={1} overflowY="hidden">
        {taskList.length === 0 ? (
          <Text dimColor>No tasks</Text>
        ) : (
          taskList.slice(0, 20).map((task) => {
            const isSelected = task.id === selectedTaskId;
            const priorityColor = priorityColors[task.priority] || 'white';
            const statusColor = statusColors[task.status] || 'white';

            return (
              <Box key={task.id} flexDirection="row" gap={1}>
                {isSelected && <Text color="cyan">&gt;</Text>}
                <Text color={priorityColor}>P{task.priority}</Text>
                <Text color={statusColor}>
                  {task.status.slice(0, 3).toUpperCase()}
                </Text>
                <Text wrap="truncate-end">
                  {task.title.slice(0, 18)}
                </Text>
              </Box>
            );
          })
        )}
        {taskList.length > 20 && (
          <Text dimColor>... and {taskList.length - 20} more</Text>
        )}
      </Box>
    </Box>
  );
}
