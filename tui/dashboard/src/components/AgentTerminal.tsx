import React, { useState, useEffect, useMemo } from 'react';
import { Box, Text, useInput } from 'ink';

export interface OutputBuffer {
  stdout: string;
  stderr: string;
}

export interface AgentTerminalProps {
  agentId: string;
  taskTitle: string;
  output: OutputBuffer;
  height?: number;
  isFocused?: boolean;
}

interface OutputLine {
  text: string;
  isStderr: boolean;
}

/**
 * AgentTerminal component displays stdout/stderr output for a selected agent.
 * Features:
 * - Scrollable output with auto-scroll to bottom
 * - Agent ID and task title in header
 * - Keyboard scrolling with j/k or arrow keys when focused
 * - Different colors for stdout (default) vs stderr (red)
 */
export const AgentTerminal: React.FC<AgentTerminalProps> = ({
  agentId,
  taskTitle,
  output,
  height = 15,
  isFocused = false,
}) => {
  const [scrollOffset, setScrollOffset] = useState(0);
  const [autoScroll, setAutoScroll] = useState(true);

  // Parse output into lines with source tracking (stdout vs stderr)
  const lines = useMemo(() => {
    const result: OutputLine[] = [];

    // Split stdout and stderr into lines
    const stdoutLines = output.stdout.split('\n');
    const stderrLines = output.stderr.split('\n');

    // Add stdout lines
    for (const line of stdoutLines) {
      if (line || stdoutLines.indexOf(line) < stdoutLines.length - 1) {
        result.push({ text: line, isStderr: false });
      }
    }

    // Add stderr lines (interleaved at the end for simplicity)
    // In a more sophisticated implementation, we could track timestamps
    for (const line of stderrLines) {
      if (line || stderrLines.indexOf(line) < stderrLines.length - 1) {
        result.push({ text: line, isStderr: true });
      }
    }

    return result;
  }, [output.stdout, output.stderr]);

  // Calculate visible lines (account for header taking 2 lines)
  const visibleHeight = height - 3; // Header (2 lines) + border
  const maxScroll = Math.max(0, lines.length - visibleHeight);

  // Auto-scroll to bottom when new output arrives
  useEffect(() => {
    if (autoScroll) {
      setScrollOffset(maxScroll);
    }
  }, [lines.length, maxScroll, autoScroll]);

  // Handle keyboard input for scrolling
  useInput(
    (input, key) => {
      if (!isFocused) return;

      // j or down arrow - scroll down
      if (input === 'j' || key.downArrow) {
        setAutoScroll(false);
        setScrollOffset((prev) => Math.min(prev + 1, maxScroll));
      }

      // k or up arrow - scroll up
      if (input === 'k' || key.upArrow) {
        setAutoScroll(false);
        setScrollOffset((prev) => Math.max(prev - 1, 0));
      }

      // Page down (ctrl+d or page down)
      if (key.pageDown || (key.ctrl && input === 'd')) {
        setAutoScroll(false);
        setScrollOffset((prev) => Math.min(prev + visibleHeight, maxScroll));
      }

      // Page up (ctrl+u or page up)
      if (key.pageUp || (key.ctrl && input === 'u')) {
        setAutoScroll(false);
        setScrollOffset((prev) => Math.max(prev - visibleHeight, 0));
      }

      // g - go to top
      if (input === 'g') {
        setAutoScroll(false);
        setScrollOffset(0);
      }

      // G - go to bottom, re-enable auto-scroll
      if (input === 'G') {
        setAutoScroll(true);
        setScrollOffset(maxScroll);
      }
    },
    { isActive: isFocused }
  );

  // Get visible lines based on scroll offset
  const visibleLines = lines.slice(scrollOffset, scrollOffset + visibleHeight);

  // Truncate agent ID for display (first 8 chars)
  const shortAgentId = agentId.slice(0, 8);

  // Calculate scroll indicator
  const scrollPercent =
    maxScroll > 0 ? Math.round((scrollOffset / maxScroll) * 100) : 100;
  const scrollIndicator = autoScroll
    ? 'AUTO'
    : `${scrollPercent}%`;

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderColor={isFocused ? 'cyan' : 'gray'}
      height={height}
    >
      {/* Header */}
      <Box justifyContent="space-between" paddingX={1}>
        <Box>
          <Text color="cyan" bold>
            Agent:{' '}
          </Text>
          <Text color="white">{shortAgentId}</Text>
          <Text color="gray"> │ </Text>
          <Text color="yellow" bold>
            Task:{' '}
          </Text>
          <Text color="white" wrap="truncate">
            {taskTitle || 'No task'}
          </Text>
        </Box>
        <Box>
          <Text color="gray">[{scrollIndicator}]</Text>
          {isFocused && <Text color="cyan"> (j/k to scroll)</Text>}
        </Box>
      </Box>

      {/* Separator */}
      <Box>
        <Text color="gray">{'─'.repeat(80)}</Text>
      </Box>

      {/* Output content */}
      <Box flexDirection="column" paddingX={1} flexGrow={1}>
        {visibleLines.length === 0 ? (
          <Text color="gray" dimColor>
            Waiting for output...
          </Text>
        ) : (
          visibleLines.map((line, index) => (
            <Text
              key={scrollOffset + index}
              color={line.isStderr ? 'red' : undefined}
              wrap="truncate"
            >
              {line.text || ' '}
            </Text>
          ))
        )}
      </Box>
    </Box>
  );
};

export default AgentTerminal;
