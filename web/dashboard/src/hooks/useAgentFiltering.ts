import { useMemo } from 'react';
import type { AgentState } from '../stores/stateStore';

export type StatusFilter = 'all' | 'running' | 'completed' | 'failed';

export interface AgentGroup {
  parent: AgentState;
  children: AgentState[];
}

export interface UseAgentFilteringOptions {
  agents: Record<string, AgentState>;
  statusFilter: StatusFilter;
  showArchived: boolean;
  activeRunId: string;
  /** When set, only show agents for this bead (overrides other filters) */
  selectedBeadId?: string | null;
}

export interface UseAgentFilteringResult {
  /** Filtered and sorted list of agents */
  filteredAgents: AgentState[];
  /** Agents grouped by parent/child relationships */
  groupedAgents: AgentGroup[];
  /** Count of archived agents (before filtering) */
  archivedCount: number;
}

/**
 * Custom hook for filtering and grouping agents.
 *
 * Handles:
 * - Filtering by selected bead (overrides all other filters)
 * - Filtering by run ID
 * - Filtering by archived status
 * - Filtering by agent status (running, completed, failed)
 * - Sorting (archived last, then by start time)
 * - Grouping by parent/child relationships
 * - Handling orphaned children (when parent is filtered out)
 */
export function useAgentFiltering({
  agents,
  statusFilter,
  showArchived,
  activeRunId,
  selectedBeadId,
}: UseAgentFilteringOptions): UseAgentFilteringResult {
  const agentList = useMemo(() => Object.values(agents), [agents]);

  // Count archived agents (for display purposes)
  const archivedCount = useMemo(() => {
    return agentList.filter((agent) => agent.archived).length;
  }, [agentList]);

  // Filter and sort agents
  const filteredAgents = useMemo(() => {
    return agentList
      .filter((agent) => {
        // If a bead is selected, ONLY show agents for that bead (override all other filters)
        if (selectedBeadId) {
          return agent.task_id === selectedBeadId;
        }

        // Filter by run ID first (if a specific run is selected)
        if (activeRunId !== '' && agent.run_id !== activeRunId) {
          return false;
        }

        // Filter by archived status
        if (!showArchived && agent.archived) {
          return false;
        }

        // Then filter by status
        if (statusFilter === 'all') return true;

        switch (statusFilter) {
          case 'running':
            return agent.status === 'running' || agent.status === 'starting';
          case 'completed':
            return agent.status === 'completed';
          case 'failed':
            return (
              agent.status === 'failed' ||
              agent.status === 'timed_out' ||
              agent.status === 'cancelled'
            );
          default:
            return true;
        }
      })
      .sort((a, b) => {
        // Archived agents go to the bottom
        if (a.archived !== b.archived) {
          return a.archived ? 1 : -1;
        }
        // Sort by start time (most recent first)
        return new Date(b.start_time).getTime() - new Date(a.start_time).getTime();
      });
  }, [agentList, statusFilter, showArchived, activeRunId, selectedBeadId]);

  // Group agents by parent/child relationships
  const groupedAgents = useMemo(() => {
    // Build a set of filtered agent IDs for quick lookup
    const filteredIds = new Set(filteredAgents.map((a) => a.id));

    // Build a map of parent_id -> children (only for filtered children)
    const childrenByParent = new Map<string, AgentState[]>();
    const orphanedChildren: AgentState[] = [];

    for (const agent of filteredAgents) {
      if (agent.parent_agent_id) {
        // Check if parent is in filtered set
        if (filteredIds.has(agent.parent_agent_id)) {
          // Parent is visible, group with parent
          const siblings = childrenByParent.get(agent.parent_agent_id) || [];
          siblings.push(agent);
          childrenByParent.set(agent.parent_agent_id, siblings);
        } else {
          // Parent is filtered out, show child as standalone
          orphanedChildren.push(agent);
        }
      }
    }

    // Get parent agents (those without parent_agent_id) with their children
    const parentGroups = filteredAgents
      .filter((agent) => !agent.parent_agent_id)
      .map((parent) => ({
        parent,
        children: childrenByParent.get(parent.id) || [],
      }));

    // Add orphaned children as standalone cards (no children of their own)
    const orphanGroups = orphanedChildren.map((orphan) => ({
      parent: orphan,
      children: [],
    }));

    return [...parentGroups, ...orphanGroups];
  }, [filteredAgents]);

  return {
    filteredAgents,
    groupedAgents,
    archivedCount,
  };
}
