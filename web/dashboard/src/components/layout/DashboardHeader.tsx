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
import type { Stats, OrchestratorState, PauseState } from '../../stores/stateStore';
import { RepoSelector } from './RepoSelector';
import { RunSelector } from './RunSelector';
import { OrchestratorStateIndicator } from './OrchestratorStateIndicator';

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

  // Orchestrator state
  orchestratorState: OrchestratorState;
  activeAgentCount: number;
  pauseState: PauseState;
  isActivating: boolean;
  isDeactivating: boolean;
  isPauseLoading: boolean;
  isResumeLoading: boolean;
  onActivate: () => void;
  onDeactivate: () => void;
  onPause: () => void;
  onResume: () => void;

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
  orchestratorState,
  activeAgentCount,
  pauseState,
  isActivating,
  isDeactivating,
  isPauseLoading,
  isResumeLoading,
  onActivate,
  onDeactivate,
  onPause,
  onResume,
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

          {/* Separator */}
          <div className="w-px h-8 bg-gray-300 dark:bg-gray-600" />

          {/* Orchestrator State Indicator */}
          <OrchestratorStateIndicator
            orchestratorState={orchestratorState}
            activeAgentCount={activeAgentCount}
            connected={connected}
            pauseState={pauseState}
            isActivating={isActivating}
            isDeactivating={isDeactivating}
            isPauseLoading={isPauseLoading}
            isResumeLoading={isResumeLoading}
            onActivate={onActivate}
            onDeactivate={onDeactivate}
            onPause={onPause}
            onResume={onResume}
          />

          {/* Separator */}
          <div className="w-px h-8 bg-gray-300 dark:bg-gray-600" />

          <RunSelector
            runs={runs}
            activeRunId={activeRunId}
            onSelect={onRunSelect}
            isLoading={isRunsLoading}
            disabled={!connected || isRepoSwitching}
          />
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
