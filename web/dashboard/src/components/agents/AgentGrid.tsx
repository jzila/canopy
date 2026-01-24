import React from 'react';
import { Activity, X, Filter, ListTodo, CheckCircle, XCircle, Archive } from 'lucide-react';
import { AgentCardGroup } from './AgentCardGroup';
import type { AgentGroup } from '../../hooks/useAgentFiltering';
import type { StatusFilter } from '../../hooks/useAgentFiltering';
import type { Stats } from '../../stores/stateStore';

export interface AgentGridProps {
  /** Grouped agents to display */
  groupedAgents: AgentGroup[];
  /** Total number of agents (for empty state message) */
  totalAgentCount: number;
  /** Current status filter */
  statusFilter: StatusFilter;
  /** Called when status filter changes */
  onStatusFilterChange: (filter: StatusFilter) => void;
  /** Stats for filter counts */
  stats: Stats;
  /** Whether to show archived agents */
  showArchivedAgents: boolean;
  /** Count of archived agents */
  archivedAgentCount: number;
  /** Called when archive toggle is clicked */
  onToggleShowArchived: () => void;
  /** Currently selected agent ID */
  selectedAgentId: string | null;
  /** Called when an agent is selected */
  onSelectAgent: (agentId: string) => void;
  /** Called when archive toggle is clicked on an agent */
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
 * - Status filter toggles
 * - Delegation to AgentCardGroup for rendering
 */
export const AgentGrid: React.FC<AgentGridProps> = ({
  groupedAgents,
  totalAgentCount,
  statusFilter,
  onStatusFilterChange,
  stats,
  showArchivedAgents,
  archivedAgentCount,
  onToggleShowArchived,
  selectedAgentId,
  onSelectAgent,
  onArchiveToggle,
  selectedBeadId,
  selectedBeadTitle,
  onClearBeadFilter,
}) => {
  const toggleFilter = (filter: StatusFilter) => {
    onStatusFilterChange(statusFilter === filter ? 'all' : filter);
  };

  // Status filter bar component
  const StatusFilterBar = () => (
    <div className="flex items-center gap-4 mb-6">
      {/* All Tasks */}
      <button
        onClick={() => onStatusFilterChange('all')}
        className={`
          flex items-center gap-3 px-5 py-3 rounded-lg font-medium transition-all cursor-pointer h-12
          ${
            statusFilter === 'all'
              ? 'bg-gray-200 dark:bg-gray-600 ring-2 ring-gray-400 dark:ring-gray-500'
              : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
          }
        `}
      >
        <ListTodo className="w-4 h-4 text-gray-600 dark:text-gray-400" />
        <span className="text-sm tracking-wider text-gray-600 dark:text-gray-300">All</span>
        <span className="text-lg font-mono font-semibold tabular-nums tracking-mono-normal text-gray-900 dark:text-gray-100">
          {stats.total_tasks}
        </span>
      </button>

      {/* Running */}
      <button
        onClick={() => toggleFilter('running')}
        className={`
          flex items-center gap-3 px-5 py-3 rounded-lg font-medium transition-all cursor-pointer h-12
          ${
            statusFilter === 'running'
              ? 'bg-blue-100 dark:bg-blue-900/50 ring-2 ring-blue-500'
              : 'bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50'
          }
        `}
      >
        <Activity className="w-4 h-4 text-blue-600 dark:text-blue-400" />
        <span className="text-sm tracking-wider text-blue-600 dark:text-blue-400">Running</span>
        <span className="text-lg font-mono font-semibold tabular-nums tracking-mono-normal text-blue-700 dark:text-blue-300">
          {stats.running_tasks}
        </span>
      </button>

      {/* Completed */}
      <button
        onClick={() => toggleFilter('completed')}
        className={`
          flex items-center gap-3 px-5 py-3 rounded-lg font-medium transition-all cursor-pointer h-12
          ${
            statusFilter === 'completed'
              ? 'bg-green-100 dark:bg-green-900/50 ring-2 ring-green-500'
              : 'bg-green-50 dark:bg-green-900/30 hover:bg-green-100 dark:hover:bg-green-900/50'
          }
        `}
      >
        <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400" />
        <span className="text-sm tracking-wider text-green-600 dark:text-green-400">
          Completed
        </span>
        <span className="text-lg font-mono font-semibold tabular-nums tracking-mono-normal text-green-700 dark:text-green-300">
          {stats.completed_tasks}
        </span>
      </button>

      {/* Failed */}
      <button
        onClick={() => toggleFilter('failed')}
        className={`
          flex items-center gap-3 px-5 py-3 rounded-lg font-medium transition-all cursor-pointer h-12
          ${
            statusFilter === 'failed'
              ? 'bg-red-100 dark:bg-red-900/50 ring-2 ring-red-500'
              : 'bg-red-50 dark:bg-red-900/30 hover:bg-red-100 dark:hover:bg-red-900/50'
          }
        `}
      >
        <XCircle className="w-4 h-4 text-red-600 dark:text-red-400" />
        <span className="text-sm tracking-wider text-red-600 dark:text-red-400">Failed</span>
        <span className="text-lg font-mono font-semibold tabular-nums tracking-mono-normal text-red-700 dark:text-red-300">
          {stats.failed_tasks}
        </span>
      </button>

      {/* Spacer */}
      <div className="flex-1" />

      {/* Show Archived Toggle */}
      <button
        onClick={onToggleShowArchived}
        className={`
          flex items-center gap-3 px-5 py-3 rounded-lg font-medium transition-all cursor-pointer h-12
          ${
            showArchivedAgents
              ? 'bg-purple-100 dark:bg-purple-900/50 ring-2 ring-purple-500'
              : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
          }
        `}
        title={showArchivedAgents ? 'Hide archived agents' : 'Show archived agents'}
      >
        <Archive
          className={`w-4 h-4 ${
            showArchivedAgents
              ? 'text-purple-600 dark:text-purple-400'
              : 'text-gray-600 dark:text-gray-400'
          }`}
        />
        <span
          className={`text-sm tracking-wider ${
            showArchivedAgents
              ? 'text-purple-600 dark:text-purple-400'
              : 'text-gray-600 dark:text-gray-400'
          }`}
        >
          {showArchivedAgents ? 'Hide' : 'Show'} Archived
        </span>
        <span
          className={`text-lg font-mono font-semibold tabular-nums tracking-mono-normal ${
            showArchivedAgents
              ? 'text-purple-700 dark:text-purple-300'
              : 'text-gray-700 dark:text-gray-300'
          }`}
        >
          {archivedAgentCount}
        </span>
      </button>
    </div>
  );

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
        <StatusFilterBar />
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
      <StatusFilterBar />
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
