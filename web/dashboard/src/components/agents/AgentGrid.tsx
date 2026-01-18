import React from 'react';
import { Activity, X, Filter } from 'lucide-react';
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
  /** Selected bead ID for filtering (optional) */
  selectedBeadId?: string | null;
  /** Title of the selected bead (optional) */
  selectedBeadTitle?: string | undefined;
  /** Called when bead filter should be cleared */
  onClearBeadFilter?: () => void;
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
  selectedBeadId,
  selectedBeadTitle,
  onClearBeadFilter,
}) => {
  // Bead filter indicator component
  const BeadFilterIndicator = () => {
    if (!selectedBeadId || !onClearBeadFilter) return null;

    return (
      <div className="mb-4 flex items-center gap-2">
        <div className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-indigo-100 dark:bg-indigo-900/50 border border-indigo-200 dark:border-indigo-800">
          <Filter className="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
          <span className="text-sm font-medium text-indigo-700 dark:text-indigo-300">
            Filtering by bead:
          </span>
          <code className="text-xs font-mono bg-indigo-200 dark:bg-indigo-800 px-1.5 py-0.5 rounded text-indigo-800 dark:text-indigo-200">
            {selectedBeadId}
          </code>
          {selectedBeadTitle && (
            <span className="text-sm text-indigo-600 dark:text-indigo-400 truncate max-w-xs">
              &mdash; {selectedBeadTitle}
            </span>
          )}
          <button
            onClick={onClearBeadFilter}
            className="ml-1 p-1 rounded hover:bg-indigo-200 dark:hover:bg-indigo-800 transition-colors"
            title="Clear bead filter"
          >
            <X className="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
          </button>
        </div>
      </div>
    );
  };

  if (groupedAgents.length === 0) {
    return (
      <div className="h-full flex flex-col">
        <BeadFilterIndicator />
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center">
            <Activity className="w-16 h-16 text-gray-300 dark:text-gray-600 mx-auto mb-5" />
            <h3 className="text-lg font-medium tracking-tight text-gray-500 dark:text-gray-400 mb-2">
              {selectedBeadId
                ? 'No agents for this bead'
                : totalAgentCount === 0
                  ? 'No Agents'
                  : `No ${statusFilter === 'all' ? '' : statusFilter} Agents`}
            </h3>
            <p className="text-sm tracking-wide text-gray-400 dark:text-gray-500">
              {selectedBeadId
                ? 'This bead has no associated worker tasks yet'
                : totalAgentCount === 0
                  ? 'Agents will appear here when tasks are running'
                  : 'Try selecting a different filter above'}
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <BeadFilterIndicator />
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
    </div>
  );
};
