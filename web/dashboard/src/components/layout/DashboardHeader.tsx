import React from 'react';
import {
  Play,
  Pause,
  Activity,
  DollarSign,
  Zap,
  GitCommit,
  FileEdit,
  Sun,
  Moon,
  CheckCircle,
  XCircle,
  ListTodo,
  Archive,
} from 'lucide-react';
import type { Repository, Run } from '../../api/client';
import type { Stats } from '../../stores/stateStore';
import type { StatusFilter } from '../../hooks/useAgentFiltering';
import { RepoSelector } from './RepoSelector';
import { RunSelector } from './RunSelector';

export interface DashboardHeaderProps {
  // Connection & state
  connected: boolean;
  isPaused: boolean;
  isPauseLoading: boolean;
  isResumeLoading: boolean;

  // Theme
  isDark: boolean;
  onToggleTheme: () => void;

  // Repository
  repositories: Repository[];
  activeRepoId: string;
  isRepoSwitching: boolean;
  onRepoSelect: (repoId: string) => void;

  // Run filtering
  runs: Run[];
  activeRunId: string;
  isRunsLoading: boolean;
  onRunSelect: (runId: string) => void;

  // Stats & filtering
  stats: Stats;
  statusFilter: StatusFilter;
  onStatusFilterChange: (filter: StatusFilter) => void;
  showArchivedAgents: boolean;
  archivedAgentCount: number;
  onToggleShowArchived: () => void;

  // Actions
  onPause: () => void;
  onResume: () => void;
}

function formatCost(cost: number): string {
  if (cost < 0.01) {
    return `$${(cost * 100).toFixed(2)}c`;
  }
  return `$${cost.toFixed(2)}`;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(1)}M`;
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`;
  }
  return tokens.toString();
}

/**
 * Dashboard header component containing:
 * - Title and connection status
 * - Repository and run selectors
 * - Theme toggle
 * - Pause/Resume controls
 * - Stats summary
 * - Status filter toggles
 */
export const DashboardHeader: React.FC<DashboardHeaderProps> = ({
  connected,
  isPaused,
  isPauseLoading,
  isResumeLoading,
  isDark,
  onToggleTheme,
  repositories,
  activeRepoId,
  isRepoSwitching,
  onRepoSelect,
  runs,
  activeRunId,
  isRunsLoading,
  onRunSelect,
  stats,
  statusFilter,
  onStatusFilterChange,
  showArchivedAgents,
  archivedAgentCount,
  onToggleShowArchived,
  onPause,
  onResume,
}) => {
  const toggleFilter = (filter: StatusFilter) => {
    onStatusFilterChange(statusFilter === filter ? 'all' : filter);
  };

  return (
    <header className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 px-8 py-5 flex-shrink-0">
      <div className="flex items-center justify-between">
        {/* Title, Repository Selector, and Connection Status */}
        <div className="flex items-center gap-6">
          <h1 className="text-2xl font-semibold tracking-mono-wide text-gray-900 dark:text-gray-100 font-mono">
            Canopy Dashboard
          </h1>
          <div className="flex items-center gap-4">
            <RepoSelector
              repositories={repositories}
              activeRepoId={activeRepoId}
              onSelect={onRepoSelect}
              isLoading={isRepoSwitching}
              disabled={!connected}
            />
            <RunSelector
              runs={runs}
              activeRunId={activeRunId}
              onSelect={onRunSelect}
              isLoading={isRunsLoading}
              disabled={!connected || isRepoSwitching}
            />
          </div>
          <div className="flex items-center gap-3">
            <div className="header-control gap-2 px-4 py-2 bg-gray-100 dark:bg-gray-700 rounded-lg">
              <div
                className={`w-2 h-2 rounded-full ${
                  connected ? 'bg-green-500 animate-pulse' : 'bg-red-500'
                }`}
              />
              <span className="text-sm font-medium tracking-wide text-gray-700 dark:text-gray-300">
                {connected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
            <button
              onClick={onToggleTheme}
              className="header-control justify-center w-10 rounded-lg bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
              title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
            >
              {isDark ? (
                <Sun className="w-5 h-5 text-yellow-500" />
              ) : (
                <Moon className="w-5 h-5 text-gray-600" />
              )}
            </button>
          </div>
        </div>

        {/* Pause/Resume Button */}
        <div className="flex items-center gap-6">
          {/* Stats Summary (non-interactive) */}
          <div className="flex items-center gap-6 text-sm">
            <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400">
              <Zap className="w-4 h-4" />
              <span className="font-mono font-medium tabular-nums tracking-mono-normal">
                {formatTokens(stats.total_tokens)}
              </span>
            </div>
            <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400">
              <DollarSign className="w-4 h-4" />
              <span className="font-mono font-medium tabular-nums tracking-mono-normal">
                {formatCost(stats.total_cost_usd)}
              </span>
            </div>
            <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400">
              <FileEdit className="w-4 h-4" />
              <span className="font-mono font-medium tabular-nums tracking-mono-normal">{stats.file_changes}</span>
            </div>
            <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400">
              <GitCommit className="w-4 h-4" />
              <span className="font-mono font-medium tabular-nums tracking-mono-normal">{stats.git_commits}</span>
            </div>
          </div>

          {isPaused ? (
            <button
              onClick={onResume}
              disabled={isResumeLoading || !connected}
              className={`
                flex items-center gap-2 px-4 py-2 bg-green-500 text-white rounded-lg
                font-medium transition-colors
                ${
                  isResumeLoading || !connected
                    ? 'opacity-50 cursor-not-allowed'
                    : 'hover:bg-green-600'
                }
              `}
            >
              <Play className="w-4 h-4" />
              {isResumeLoading ? 'Resuming...' : 'Resume'}
            </button>
          ) : (
            <button
              onClick={onPause}
              disabled={isPauseLoading || !connected}
              className={`
                flex items-center gap-2 px-4 py-2 bg-orange-500 text-white rounded-lg
                font-medium transition-colors
                ${
                  isPauseLoading || !connected
                    ? 'opacity-50 cursor-not-allowed'
                    : 'hover:bg-orange-600'
                }
              `}
            >
              <Pause className="w-4 h-4" />
              {isPauseLoading ? 'Pausing...' : 'Pause'}
            </button>
          )}
        </div>
      </div>

      {/* Stats Filter Toggles */}
      <div className="flex items-center gap-4 mt-6">
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
    </header>
  );
};
