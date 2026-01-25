import React, { useEffect, useState } from 'react';
import { Clock, Zap, DollarSign, XCircle, GitCommit, Archive, ExternalLink, GitMerge, AlertTriangle, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import type { AgentState, LifecycleState } from '../../stores/stateStore';
import { useStateStore } from '../../stores/stateStore';
import { killAgent, archiveAgent } from '../../api/client';
import { AgentChainTimeline } from './AgentChainTimeline';
import { ValidationStatusBadge } from './ValidationStatusBadge';

interface AgentCardProps {
  agent: AgentState;
  onSelect: (agentId: string) => void;
  isSelected: boolean;
  onArchiveToggle?: (agentId: string, archived: boolean) => void;
}

// Format retry badge text: "Attempt N of M" or "Retry N" for infinite retries
const formatRetryBadge = (attempt: number, maxRetries: number): string | null => {
  // Only show badge if this is a retry (attempt > 1)
  if (attempt <= 1) return null;

  if (maxRetries === -1) {
    // Infinite retries: show "Retry N" (attempt 2 = Retry 1, etc.)
    return `Retry ${attempt - 1}`;
  }

  // Finite retries: show "Attempt N of M+1" (maxRetries + 1 = total attempts)
  return `Attempt ${attempt} of ${maxRetries + 1}`;
};

// Lifecycle state colors - more detailed states from the state machine
const LIFECYCLE_COLORS: Record<LifecycleState, string> = {
  starting: 'bg-yellow-500',
  running: 'bg-blue-500',
  queued_for_merge: 'bg-purple-500',
  merging: 'bg-indigo-500',
  resolving: 'bg-amber-500',
  validating: 'bg-cyan-500',
  repairing: 'bg-orange-500',
  merge_failed: 'bg-red-500',
  completed: 'bg-green-500',
  failed: 'bg-red-500',
  needs_attention: 'bg-orange-600',
  cancelled: 'bg-gray-500',
  timed_out: 'bg-orange-500',
};

// User-friendly display labels for lifecycle states
const LIFECYCLE_LABELS: Record<LifecycleState, string> = {
  starting: 'Starting',
  running: 'Running',
  queued_for_merge: 'Queued',
  merging: 'Merging',
  resolving: 'Resolving',
  validating: 'Validating',
  repairing: 'Repairing',
  merge_failed: 'Merge Failed',
  completed: 'Completed',
  failed: 'Failed',
  needs_attention: 'Needs Attention',
  cancelled: 'Cancelled',
  timed_out: 'Timed Out',
};

// Get display info for lifecycle state with optional context (queue pos, repair attempt)
const getLifecycleDisplay = (
  state: LifecycleState,
  queuePos?: number,
  repairAttempts?: number,
  maxRepairAttempts?: number
): { label: string; color: string } => {
  const baseLabel = LIFECYCLE_LABELS[state];
  const color = LIFECYCLE_COLORS[state];

  // Add context for specific states
  if (state === 'queued_for_merge' && queuePos !== undefined && queuePos > 0) {
    return { label: `Queued (#${queuePos})`, color };
  }

  if (state === 'repairing' && repairAttempts !== undefined) {
    const maxAttempts = maxRepairAttempts ?? 3;
    return { label: `Repairing (${repairAttempts}/${maxAttempts})`, color };
  }

  return { label: baseLabel, color };
};

// Derive lifecycle state from legacy fields for backwards compatibility
// Used when lifecycle_state is not present (older backend versions)
const deriveLifecycleState = (agent: AgentState): LifecycleState => {
  // First check validation/repair status if merge is complete
  if (agent.merge_status === 'merged' || agent.merge_status === 'merged_needs_repair') {
    if (agent.validation_status === 'running') return 'validating';
    if (agent.validation_status === 'repairing') return 'repairing';
    if (agent.validation_status === 'failed') return 'needs_attention';
    // If validation passed or not present, fall through
  }

  // Check merge status
  if (agent.merge_status === 'pending') return 'queued_for_merge';
  if (agent.merge_status === 'acquiring' || agent.merge_status === 'merging') return 'merging';
  if (agent.merge_status === 'resolving') return 'resolving';
  if (agent.merge_status === 'failed') return 'merge_failed';

  // Check agent status
  switch (agent.status) {
    case 'starting': return 'starting';
    case 'running': return 'running';
    case 'completed': return 'completed';
    case 'failed': return 'failed';
    case 'cancelled': return 'cancelled';
    case 'timed_out': return 'timed_out';
  }

  // Default fallback
  return 'running';
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
  return cost.toFixed(2);
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
  const [showAgentChain, setShowAgentChain] = useState(false);
  const setHighlightedTask = useStateStore((state) => state.setHighlightedTask);
  const tasks = useStateStore((state) => state.tasks);
  const agents = useStateStore((state) => state.agents);

  // Get child agents for this agent (resolvers, repair agents)
  const childAgents = Object.values(agents).filter(
    a => a.parent_agent_id === agent.id
  );

  // Get prior attempts for this task (agents with same task_id but earlier attempt numbers)
  const priorAttempts = Object.values(agents).filter(a =>
    a.task_id === agent.task_id &&
    a.id !== agent.id &&
    (a.attempt ?? 1) < (agent.attempt ?? 1)
  ).sort((a, b) => (a.attempt ?? 1) - (b.attempt ?? 1));

  // Check if there's an active resolver for this agent's task
  const activeResolver = childAgents.find(
    child => (child.status === 'running' || child.status === 'starting') &&
             !child.task_id.includes('repair')
  );
  const hasActiveResolver = Boolean(activeResolver);

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

  // Check if we have agent chain data to display
  const hasAgentChainData = Boolean(
    agent.merge_status ||
    agent.validation_status ||
    agent.repair_attempts ||
    childAgents.length > 0 ||
    priorAttempts.length > 0
  );

  // Get failed validation step name if applicable
  const failedValidationStep = agent.validation_steps?.find(
    step => step.status === 'failed'
  )?.name;

  // Use lifecycle_state if available, otherwise derive from legacy fields
  const lifecycleState = agent.lifecycle_state ?? deriveLifecycleState(agent);
  const lifecycleDisplay = getLifecycleDisplay(
    lifecycleState,
    agent.merge_queue_pos,
    agent.repair_attempts,
    3 // Default max repair attempts
  );

  const isRunning = agent.status === 'running' || agent.status === 'starting';
  const isFinished = agent.status === 'completed' || agent.status === 'failed' || agent.status === 'timed_out' || agent.status === 'cancelled';

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
            <span className={`inline-flex items-center px-2.5 py-1 rounded text-xs font-medium tracking-wider text-white ${hasActiveResolver ? 'bg-amber-500' : lifecycleDisplay.color}`}>
              {hasActiveResolver ? 'Resolving' : lifecycleDisplay.label}
            </span>
            {/* Retry indicator badge - only shown for retried tasks */}
            {agent.attempt !== undefined && agent.max_retries !== undefined && formatRetryBadge(agent.attempt, agent.max_retries) && (
              <span
                className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium tracking-wider bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200"
                title={`This is attempt ${agent.attempt}${agent.max_retries === -1 ? ' (infinite retries)' : ` of ${agent.max_retries + 1}`}`}
              >
                <RefreshCw className="w-3 h-3" />
                {formatRetryBadge(agent.attempt, agent.max_retries)}
              </span>
            )}
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

        {/* Show merge_commits_applied for merged tasks (most accurate), fall back to agent.commits */}
        {(() => {
          const commitCount = (agent.merge_status === 'merged' || agent.merge_status === 'merged_needs_repair')
            ? (agent.merge_commits_applied ?? agent.commits)
            : agent.commits;
          return commitCount > 0 ? (
            <div className="flex items-center gap-2.5 text-blue-400" title={`${commitCount} git commit${commitCount !== 1 ? 's' : ''}`}>
              <GitCommit className="w-4 h-4 flex-shrink-0" />
              <span className="font-mono tabular-nums tracking-mono-normal">{commitCount}</span>
            </div>
          ) : null;
        })()}
      </div>

      {/* Merge and validation status indicators */}
      {hasAgentChainData && (
        <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
          {/* Merge status */}
          {agent.merge_status === 'merged' || agent.merge_status === 'merged_needs_repair' ? (
            <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400" title={`Merged${agent.merge_commits_applied ? ` (${agent.merge_commits_applied} commits)` : ''}`}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="tracking-wide">Merged</span>
              {agent.merge_commits_applied ? <span className="tabular-nums">({agent.merge_commits_applied})</span> : null}
            </div>
          ) : agent.merge_status === 'skipped' ? (
            <div className="flex items-center gap-1.5 text-green-600 dark:text-green-400" title={agent.merge_error || 'Work already done - no changes needed'}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="tracking-wide">No Changes</span>
            </div>
          ) : agent.merge_status === 'failed' ? (
            <div className="flex items-center gap-1.5 text-red-600 dark:text-red-400" title={agent.merge_error || 'Merge failed'}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="tracking-wide">Merge Failed</span>
            </div>
          ) : null}

          {/* Conflict indicator */}
          {agent.merge_had_conflict && (
            <div className="flex items-center gap-1.5 text-yellow-600 dark:text-yellow-400" title="Merge had conflicts">
              <AlertTriangle className="w-3.5 h-3.5" />
              <span className="tracking-wide">Conflict</span>
            </div>
          )}

          {/* Resolver indicator */}
          {agent.merge_resolver_spawned && (
            <span className="text-purple-600 dark:text-purple-400 tracking-wide" title="Resolver agent was spawned">
              (Resolved)
            </span>
          )}

          {/* Validation status badge */}
          {agent.validation_status && (
            <ValidationStatusBadge
              status={agent.validation_status}
              repairAttempts={agent.repair_attempts}
              failedStep={failedValidationStep}
            />
          )}

          {/* Agent chain toggle */}
          {hasAgentChainData && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                setShowAgentChain(!showAgentChain);
              }}
              className="flex items-center gap-1 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 ml-auto"
              title={showAgentChain ? 'Hide agent chain' : 'Show agent chain'}
            >
              {showAgentChain ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
              <span className="tracking-wide">Details</span>
            </button>
          )}
        </div>
      )}

      {/* Agent chain timeline (expandable) */}
      {showAgentChain && hasAgentChainData && (
        <AgentChainTimeline agent={agent} childAgents={childAgents} priorAttempts={priorAttempts} onSelectAgent={onSelect} />
      )}

      {agent.error && (
        <div className="mt-4 text-xs text-red-600 dark:text-red-400 truncate" title={agent.error}>
          Error: {agent.error}
        </div>
      )}
    </div>
  );
};
