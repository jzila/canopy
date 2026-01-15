import React from 'react';
import { Activity } from 'lucide-react';
import { AgentCardGroup } from './AgentCardGroup';
import type { AgentGroup } from '../../hooks/useAgentFiltering';
import type { StatusFilter } from '../../hooks/useAgentFiltering';

export interface AgentGridProps {
  /** Grouped agents to display */
  groupedAgents: AgentGroup[];
  /** Total number of agents (for empty state message) */
  totalAgentCount: number;
  /** Current status filter (for empty state message) */
  statusFilter: StatusFilter;
  /** Currently selected agent ID */
  selectedAgentId: string | null;
  /** Called when an agent is selected */
  onSelectAgent: (agentId: string) => void;
  /** Called when archive toggle is clicked */
  onArchiveToggle: (agentId: string, archived: boolean) => void;
}

/**
 * Grid layout for displaying agent cards.
 *
 * Handles:
 * - Responsive grid layout (1-4 columns based on viewport)
 * - Empty state display
 * - Delegation to AgentCardGroup for rendering
 */
export const AgentGrid: React.FC<AgentGridProps> = ({
  groupedAgents,
  totalAgentCount,
  statusFilter,
  selectedAgentId,
  onSelectAgent,
  onArchiveToggle,
}) => {
  if (groupedAgents.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center">
          <Activity className="w-16 h-16 text-gray-300 dark:text-gray-600 mx-auto mb-5" />
          <h3 className="text-lg font-medium tracking-tight text-gray-500 dark:text-gray-400 mb-2">
            {totalAgentCount === 0
              ? 'No Agents'
              : `No ${statusFilter === 'all' ? '' : statusFilter} Agents`}
          </h3>
          <p className="text-sm tracking-wide text-gray-400 dark:text-gray-500">
            {totalAgentCount === 0
              ? 'Agents will appear here when tasks are running'
              : 'Try selecting a different filter above'}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5 items-start">
      {groupedAgents.map(({ parent, children }) => (
        <AgentCardGroup
          key={parent.id}
          parentAgent={parent}
          childAgents={children}
          onSelect={onSelectAgent}
          selectedAgentId={selectedAgentId}
          onArchiveToggle={onArchiveToggle}
        />
      ))}
    </div>
  );
};
