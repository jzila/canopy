import React from 'react';
import {
  DollarSign,
  Zap,
  GitCommit,
  FileEdit,
  Sun,
  Moon,
} from 'lucide-react';
import type { Repository, Run } from '../../api/client';
import type { Stats } from '../../stores/stateStore';
import { RepoSelector } from './RepoSelector';
import { RunSelector } from './RunSelector';

export interface DashboardHeaderProps {
  // Connection & state
  connected: boolean;

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

  // Stats
  stats: Stats;
}

function formatCost(cost: number): string {
  return cost.toFixed(2);
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
 * - Stats summary
 */

export const DashboardHeader: React.FC<DashboardHeaderProps> = ({
  connected,
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
}) => {
  return (
    <header className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 px-8 py-5 flex-shrink-0">
      <div className="flex items-center">
        {/* Title */}
        <h1 className="text-3xl font-light tracking-mono-wide text-gray-900 dark:text-gray-100 font-mono">
          Canopy
        </h1>

        {/* Header Controls - centered */}
        <div className="flex-1 flex items-center justify-center gap-3">
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
          <div className="header-control gap-2 px-4 bg-gray-100 dark:bg-gray-700 rounded-lg">
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
            className="header-control justify-center w-12 rounded-lg bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {isDark ? (
              <Sun className="w-5 h-5 text-yellow-500" />
            ) : (
              <Moon className="w-5 h-5 text-gray-600" />
            )}
          </button>
        </div>

        {/* Stats Summary */}
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
      </div>
    </header>
  );
};
