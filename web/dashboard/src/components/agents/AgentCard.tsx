import React, { useEffect, useState } from 'react';
import { Clock, Zap, DollarSign, XCircle, GitCommit, Archive, ExternalLink, GitMerge, AlertTriangle } from 'lucide-react';
import type { AgentState } from '../../stores/stateStore';
import { useStateStore } from '../../stores/stateStore';
import { killAgent, archiveAgent } from '../../api/client';

interface AgentCardProps {
  agent: AgentState;
  onSelect: (agentId: string) => void;
  isSelected: boolean;
  onArchiveToggle?: (agentId: string, archived: boolean) => void;
}

const STATUS_COLORS: Record<string, string> = {
  starting: 'bg-yellow-500',
  running: 'bg-blue-500',
  completed: 'bg-green-500',
  failed: 'bg-red-500',
  timed_out: 'bg-orange-500',
  cancelled: 'bg-gray-500',
};

const formatElapsedTime = (startTime: string, endTime: string | null): string => {
  const start = new Date(startTime).getTime();
  const end = endTime ? new Date(endTime).getTime() : Date.now();
  const elapsed = Math.floor((end - start) / 1000);

  const hours = Math.floor(elapsed / 3600);
  const minutes = Math.floor((elapsed % 3600) / 60);
  const seconds = elapsed % 60;

  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  } else if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  } else {
    return `${seconds}s`;
  }
};

const formatTokenCount = (count: number): string => {
  if (count >= 1000000) {
    return `${(count / 1000000).toFixed(1)}M`;
  } else if (count >= 1000) {
    return `${(count / 1000).toFixed(1)}K`;
  }
  return count.toString();
};

const formatCost = (cost: number): string => {
  if (cost < 0.01) {
    return `$${(cost * 100).toFixed(2)}¢`;
  }
  return `$${cost.toFixed(2)}`;
};

const truncateId = (id: string, length: number = 8): string => {
  return id.slice(0, length);
};

export const AgentCard: React.FC<AgentCardProps> = ({
  agent,
  onSelect,
  isSelected,
  onArchiveToggle,
}) => {
  const [elapsedTime, setElapsedTime] = useState<string>(
    formatElapsedTime(agent.start_time, agent.end_time)
  );
  const [isKilling, setIsKilling] = useState(false);
  const [isArchiving, setIsArchiving] = useState(false);
  const setHighlightedTask = useStateStore((state) => state.setHighlightedTask);
  const tasks = useStateStore((state) => state.tasks);

  // Update elapsed time every second for running agents
  useEffect(() => {
    if (agent.status === 'running' || agent.status === 'starting') {
      const interval = setInterval(() => {
        setElapsedTime(formatElapsedTime(agent.start_time, agent.end_time));
      }, 1000);

      return () => clearInterval(interval);
    } else {
      setElapsedTime(formatElapsedTime(agent.start_time, agent.end_time));
    }
  }, [agent.start_time, agent.end_time, agent.status]);

  const handleKill = async (e: React.MouseEvent) => {
    e.stopPropagation();

    if (isKilling) return;

    try {
      setIsKilling(true);
      await killAgent(agent.id);
    } catch (error) {
      console.error('Failed to kill agent:', error);
    } finally {
      setIsKilling(false);
    }
  };

  const handleArchiveToggle = async (e: React.MouseEvent) => {
    e.stopPropagation();

    if (isArchiving) return;

    try {
      setIsArchiving(true);
      const newArchived = !agent.archived;
      await archiveAgent(agent.id, newArchived);
      onArchiveToggle?.(agent.id, newArchived);
    } catch (error) {
      console.error('Failed to archive agent:', error);
    } finally {
      setIsArchiving(false);
    }
  };

  const handleCardClick = () => {
    onSelect(agent.id);
  };

  const handleTaskIdClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    // Highlight the task in BeadsPane if it exists
    if (tasks[agent.task_id]) {
      setHighlightedTask(agent.task_id);
      // Clear highlight after 3 seconds
      setTimeout(() => setHighlightedTask(null), 3000);
    }
  };

  // Check if task exists in the BeadsPane (incomplete tasks only)
  const taskExistsInBeads = Boolean(tasks[agent.task_id]);

  const isRunning = agent.status === 'running' || agent.status === 'starting';
  const isFinished = agent.status === 'completed' || agent.status === 'failed' || agent.status === 'timed_out' || agent.status === 'cancelled';
  const statusColor = STATUS_COLORS[agent.status] || 'bg-gray-500';

  return (
    <div
      onClick={handleCardClick}
      className={`
        p-5 rounded-lg border-2 transition-all cursor-pointer
        ${agent.archived
          ? 'border-gray-300 dark:border-gray-600 bg-gray-100 dark:bg-gray-900 opacity-60'
          : isSelected
            ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/30 shadow-lg'
            : 'border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 hover:border-gray-300 dark:hover:border-gray-600 hover:shadow-md'
        }
      `}
    >
      <div className="flex items-start justify-between mb-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 mb-2.5 h-7">
            {agent.archived && (
              <span className="inline-flex items-center px-2.5 py-1 rounded text-xs font-medium tracking-wider bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                Archived
              </span>
            )}
            {taskExistsInBeads ? (
              <button
                onClick={handleTaskIdClick}
                className="inline-flex items-center gap-1.5 text-sm font-mono tracking-mono-normal text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 hover:underline transition-colors"
                title="Click to highlight in Beads pane"
              >
                {agent.task_id}
                <ExternalLink className="w-3 h-3" />
              </button>
            ) : (
              <code className="text-sm font-mono tracking-mono-normal text-gray-500 dark:text-gray-400">
                {agent.task_id}
              </code>
            )}
            <span className={`inline-flex items-center px-2.5 py-1 rounded text-xs font-medium tracking-wider text-white ${statusColor}`}>
              {agent.status}
            </span>
          </div>
          <h3 className={`text-sm font-medium tracking-wide leading-relaxed truncate ${agent.archived ? 'text-gray-500 dark:text-gray-400 line-through' : 'text-gray-900 dark:text-gray-100'}`}>
            {agent.task_title}
          </h3>
        </div>

        <div className="flex items-center gap-1">
          {isFinished && (
            <button
              onClick={handleArchiveToggle}
              disabled={isArchiving}
              className={`
                p-1 rounded transition-colors
                ${isArchiving ? 'opacity-50 cursor-not-allowed' : ''}
                ${agent.archived
                  ? 'text-purple-600 hover:text-purple-800 hover:bg-purple-100 dark:text-purple-400 dark:hover:text-purple-200 dark:hover:bg-purple-900'
                  : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:text-gray-500 dark:hover:text-gray-300 dark:hover:bg-gray-700'
                }
              `}
              title={agent.archived ? 'Unarchive agent' : 'Archive agent'}
            >
              <Archive className="w-4 h-4" />
            </button>
          )}
          {isRunning && (
            <button
              onClick={handleKill}
              disabled={isKilling}
              className={`
                p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/30 transition-colors
                ${isKilling ? 'opacity-50 cursor-not-allowed' : ''}
              `}
              title="Kill agent"
            >
              <XCircle className="w-5 h-5 text-red-600 dark:text-red-400" />
            </button>
          )}
        </div>
      </div>

      <div className="grid grid-cols-4 gap-4 text-xs h-6">
        <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400" title="Elapsed time">
          <Clock className="w-4 h-4 flex-shrink-0" />
          <span className="font-mono tabular-nums tracking-mono-normal">{elapsedTime}</span>
        </div>

        <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400" title="Token usage">
          <Zap className="w-4 h-4 flex-shrink-0" />
          <span className="font-mono tabular-nums tracking-mono-normal">{formatTokenCount(agent.token_usage.total_tokens)}</span>
        </div>

        <div className="flex items-center gap-2.5 text-gray-600 dark:text-gray-400" title="Cost (USD)">
          <DollarSign className="w-4 h-4 flex-shrink-0" />
          <span className="font-mono tabular-nums tracking-mono-normal">{formatCost(agent.token_usage.cost_usd)}</span>
        </div>

        {agent.commits > 0 && (
          <div className="flex items-center gap-2.5 text-blue-400" title={`${agent.commits} git commit${agent.commits !== 1 ? 's' : ''}`}>
            <GitCommit className="w-4 h-4 flex-shrink-0" />
            <span className="font-mono tabular-nums tracking-mono-normal">{agent.commits}</span>
          </div>
        )}
      </div>

      {/* Merge status indicator for historical data */}
      {agent.merge_status && (
        <div className="mt-3 flex items-center gap-3 text-xs">
          {agent.merge_status === 'merged' ? (
            <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400" title={`Merged${agent.merge_commits_applied ? ` (${agent.merge_commits_applied} commits)` : ''}`}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="tracking-wide">Merged</span>
              {agent.merge_commits_applied ? <span className="tabular-nums">({agent.merge_commits_applied})</span> : null}
            </div>
          ) : agent.merge_status === 'failed' ? (
            <div className="flex items-center gap-1.5 text-red-600 dark:text-red-400" title={agent.merge_error || 'Merge failed'}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="tracking-wide">Merge Failed</span>
            </div>
          ) : null}
          {agent.merge_had_conflict && (
            <div className="flex items-center gap-1.5 text-yellow-600 dark:text-yellow-400" title="Merge had conflicts">
              <AlertTriangle className="w-3.5 h-3.5" />
              <span className="tracking-wide">Conflict</span>
            </div>
          )}
          {agent.merge_resolver_spawned && (
            <span className="text-purple-600 dark:text-purple-400 tracking-wide" title="Resolver agent was spawned">
              (Resolved)
            </span>
          )}
        </div>
      )}

      {agent.error && (
        <div className="mt-4 text-xs text-red-600 dark:text-red-400 truncate" title={agent.error}>
          Error: {agent.error}
        </div>
      )}
    </div>
  );
};
