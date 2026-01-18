import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useAgentFiltering } from './useAgentFiltering';
import type { AgentState } from '../stores/stateStore';

function createMockAgent(overrides: Partial<AgentState>): AgentState {
  return {
    id: 'agent-1',
    task_id: 'task-1',
    task_title: 'Test Task',
    status: 'running',
    start_time: new Date().toISOString(),
    end_time: null,
    duration: 0,
    output: { stdout: '', stderr: '' },
    liveFeed: [],
    token_usage: { input_tokens: 0, output_tokens: 0, cache_creation_input_tokens: 0, cache_read_input_tokens: 0, total_tokens: 0, cost_usd: 0 },
    exit_code: 0,
    error: '',
    changes: 0,
    commits: 0,
    git_commits: [],
    archived: false,
    ...overrides,
  };
}

describe('useAgentFiltering', () => {
  describe('status filtering', () => {
    it('returns all agents when filter is "all"', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', status: 'running' }),
        'agent-2': createMockAgent({ id: 'agent-2', status: 'completed' }),
        'agent-3': createMockAgent({ id: 'agent-3', status: 'failed' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(3);
    });

    it('filters to only running agents', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', status: 'running' }),
        'agent-2': createMockAgent({ id: 'agent-2', status: 'starting' }),
        'agent-3': createMockAgent({ id: 'agent-3', status: 'completed' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'running',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(2);
      expect(result.current.filteredAgents.every((a) => a.status === 'running' || a.status === 'starting')).toBe(true);
    });

    it('filters to only completed agents', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', status: 'running' }),
        'agent-2': createMockAgent({ id: 'agent-2', status: 'completed' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'completed',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(1);
      expect(result.current.filteredAgents[0]!.status).toBe('completed');
    });

    it('filters to failed agents (including timed_out and cancelled)', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', status: 'failed' }),
        'agent-2': createMockAgent({ id: 'agent-2', status: 'timed_out' }),
        'agent-3': createMockAgent({ id: 'agent-3', status: 'cancelled' }),
        'agent-4': createMockAgent({ id: 'agent-4', status: 'completed' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'failed',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(3);
    });
  });

  describe('archived filtering', () => {
    it('excludes archived agents when showArchived is false', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', archived: false }),
        'agent-2': createMockAgent({ id: 'agent-2', archived: true }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(1);
      expect(result.current.filteredAgents[0]!.archived).toBe(false);
    });

    it('includes archived agents when showArchived is true', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', archived: false }),
        'agent-2': createMockAgent({ id: 'agent-2', archived: true }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: true,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(2);
    });

    it('counts archived agents correctly', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', archived: false }),
        'agent-2': createMockAgent({ id: 'agent-2', archived: true }),
        'agent-3': createMockAgent({ id: 'agent-3', archived: true }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.archivedCount).toBe(2);
    });
  });

  describe('run filtering', () => {
    it('filters by run ID when specified', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', run_id: 'run-1' }),
        'agent-2': createMockAgent({ id: 'agent-2', run_id: 'run-2' }),
        'agent-3': createMockAgent({ id: 'agent-3', run_id: 'run-1' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: 'run-1',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(2);
      expect(result.current.filteredAgents.every((a) => a.run_id === 'run-1')).toBe(true);
    });

    it('shows all runs when activeRunId is empty', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1', run_id: 'run-1' }),
        'agent-2': createMockAgent({ id: 'agent-2', run_id: 'run-2' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents).toHaveLength(2);
    });
  });

  describe('sorting', () => {
    it('sorts archived agents to the bottom', () => {
      const agents = {
        'agent-1': createMockAgent({
          id: 'agent-1',
          archived: true,
          start_time: '2024-01-03T00:00:00Z',
        }),
        'agent-2': createMockAgent({
          id: 'agent-2',
          archived: false,
          start_time: '2024-01-01T00:00:00Z',
        }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: true,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents[0]!.id).toBe('agent-2');
      expect(result.current.filteredAgents[1]!.id).toBe('agent-1');
    });

    it('sorts by start time (most recent first) within same archive status', () => {
      const agents = {
        'agent-1': createMockAgent({
          id: 'agent-1',
          start_time: '2024-01-01T00:00:00Z',
        }),
        'agent-2': createMockAgent({
          id: 'agent-2',
          start_time: '2024-01-03T00:00:00Z',
        }),
        'agent-3': createMockAgent({
          id: 'agent-3',
          start_time: '2024-01-02T00:00:00Z',
        }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.filteredAgents[0]!.id).toBe('agent-2');
      expect(result.current.filteredAgents[1]!.id).toBe('agent-3');
      expect(result.current.filteredAgents[2]!.id).toBe('agent-1');
    });
  });

  describe('parent/child grouping', () => {
    it('groups children under their parent', () => {
      const agents = {
        'parent-1': createMockAgent({ id: 'parent-1' }),
        'child-1': createMockAgent({ id: 'child-1', parent_agent_id: 'parent-1' }),
        'child-2': createMockAgent({ id: 'child-2', parent_agent_id: 'parent-1' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.groupedAgents).toHaveLength(1);
      expect(result.current.groupedAgents[0]!.parent.id).toBe('parent-1');
      expect(result.current.groupedAgents[0]!.children).toHaveLength(2);
    });

    it('shows orphaned children as standalone when parent is filtered out', () => {
      const agents = {
        'parent-1': createMockAgent({ id: 'parent-1', status: 'completed' }),
        'child-1': createMockAgent({ id: 'child-1', parent_agent_id: 'parent-1', status: 'running' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'running',
          showArchived: false,
          activeRunId: '',
        })
      );

      // Child should appear as standalone since parent is filtered out
      expect(result.current.groupedAgents).toHaveLength(1);
      expect(result.current.groupedAgents[0]!.parent.id).toBe('child-1');
      expect(result.current.groupedAgents[0]!.children).toHaveLength(0);
    });

    it('creates groups for agents without parents', () => {
      const agents = {
        'agent-1': createMockAgent({ id: 'agent-1' }),
        'agent-2': createMockAgent({ id: 'agent-2' }),
      };

      const { result } = renderHook(() =>
        useAgentFiltering({
          agents,
          statusFilter: 'all',
          showArchived: false,
          activeRunId: '',
        })
      );

      expect(result.current.groupedAgents).toHaveLength(2);
      expect(result.current.groupedAgents.every((g) => g.children.length === 0)).toBe(true);
    });
  });
});
