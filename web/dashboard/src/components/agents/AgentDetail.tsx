import React, { useState } from 'react';
import {
  Clock,
  Hash,
  FileText,
  DollarSign,
  Coins,
  GitMerge,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Timer,
  Activity,
  ChevronDown,
  ChevronRight,
  RefreshCw,
  Users,
  ExternalLink,
} from 'lucide-react';
import { CommitList } from './CommitList';
import type { AgentState, MergeStatus } from '../../stores/stateStore';
import { useStateStore } from '../../stores/stateStore';

interface AgentDetailProps {
  agent: AgentState;
  onSelectAgent?: (agentId: string) => void;
}

// Format duration in a readable way
const formatDuration = (seconds: number): string => {
  if (seconds < 60) {
    return `${seconds.toFixed(1)}s`;
  }
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins < 60) {
    return `${mins}m ${secs.toFixed(0)}s`;
  }
  const hours = Math.floor(mins / 60);
  const remainingMins = mins % 60;
  return `${hours}h ${remainingMins}m`;
};

// Format timestamp for display
const formatTimestamp = (timestamp: string): string => {
  try {
    const date = new Date(timestamp);
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    });
  } catch {
    return timestamp;
  }
};

// Format cost as USD
const formatCost = (cost: number): string => {
  if (cost < 0.01) {
    return `$${cost.toFixed(4)}`;
  }
  return `$${cost.toFixed(2)}`;
};

// Format token count with commas
const formatTokens = (tokens: number): string => {
  return tokens.toLocaleString();
};

// Status badge component
const StatusBadge: React.FC<{ status: AgentState['status'] }> = ({ status }) => {
  const statusConfig: Record<AgentState['status'], { icon: React.ReactNode; color: string; label: string }> = {
    starting: {
      icon: <Activity className="w-4 h-4 animate-pulse" />,
      color: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300',
      label: 'Starting',
    },
    running: {
      icon: <Activity className="w-4 h-4 animate-pulse" />,
      color: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300',
      label: 'Running',
    },
    completed: {
      icon: <CheckCircle2 className="w-4 h-4" />,
      color: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300',
      label: 'Completed',
    },
    failed: {
      icon: <XCircle className="w-4 h-4" />,
      color: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300',
      label: 'Failed',
    },
    timed_out: {
      icon: <Timer className="w-4 h-4" />,
      color: 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300',
      label: 'Timed Out',
    },
    cancelled: {
      icon: <XCircle className="w-4 h-4" />,
      color: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
      label: 'Cancelled',
    },
  };

  const config = statusConfig[status];
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-sm font-medium ${config.color}`}>
      {config.icon}
      {config.label}
    </span>
  );
};

// Merge status badge
const MergeStatusBadge: React.FC<{ status: MergeStatus; queuePos?: number; error?: string }> = ({
  status,
  queuePos,
  error,
}) => {
  const statusConfig: Record<MergeStatus, { color: string; label: string; icon: React.ReactNode }> = {
    pending: { color: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300', label: 'Pending', icon: <GitMerge className="w-4 h-4" /> },
    acquiring: { color: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300', label: 'Acquiring Lock', icon: <GitMerge className="w-4 h-4" /> },
    merging: { color: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300', label: 'Merging', icon: <GitMerge className="w-4 h-4" /> },
    resolving: { color: 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300', label: 'Resolving Conflicts', icon: <GitMerge className="w-4 h-4" /> },
    merged: { color: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300', label: 'Merged', icon: <GitMerge className="w-4 h-4" /> },
    merged_needs_repair: { color: 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300', label: 'Merged (Needs Repair)', icon: <GitMerge className="w-4 h-4" /> },
    failed: { color: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300', label: 'Merge Failed', icon: <GitMerge className="w-4 h-4" /> },
    skipped: { color: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300', label: 'No Changes Needed', icon: <CheckCircle2 className="w-4 h-4" /> },
  };

  const config = statusConfig[status];
  const isSkipped = status === 'skipped';
  return (
    <div className="space-y-1">
      <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-sm font-medium ${config.color}`}>
        {config.icon}
        {config.label}
        {queuePos !== undefined && queuePos > 0 && ` (Queue: #${queuePos})`}
      </span>
      {error && (
        <div className={`text-xs flex items-start gap-1 mt-1 ${isSkipped ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
          {!isSkipped && <AlertCircle className="w-3 h-3 mt-0.5 flex-shrink-0" />}
          {isSkipped && <CheckCircle2 className="w-3 h-3 mt-0.5 flex-shrink-0" />}
          <span>{error}</span>
        </div>
      )}
    </div>
  );
};

// Detail row component
const DetailRow: React.FC<{
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
  monospace?: boolean;
}> = ({ icon, label, value, monospace }) => (
  <div className="flex items-start gap-3 py-2">
    <div className="flex-shrink-0 text-gray-400 dark:text-gray-500 mt-0.5">{icon}</div>
    <div className="flex-1 min-w-0">
      <div className="text-xs text-gray-500 dark:text-gray-400 mb-0.5">{label}</div>
      <div className={`text-sm text-gray-800 dark:text-gray-200 ${monospace ? 'font-mono' : ''}`}>{value}</div>
    </div>
  </div>
);

// Section component
const Section: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
  <div className="mb-4">
    <h4 className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">{title}</h4>
    <div className="bg-gray-50 dark:bg-gray-800/50 rounded-lg p-3">{children}</div>
  </div>
);

// Collapsible section component for expandable content
const CollapsibleSection: React.FC<{
  title: string;
  badge?: number;
  defaultExpanded?: boolean;
  children: React.ReactNode;
}> = ({ title, badge, defaultExpanded = false, children }) => {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  return (
    <div className="mb-4">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center justify-between text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
      >
        <div className="flex items-center gap-2">
          {isExpanded ? (
            <ChevronDown className="w-4 h-4" />
          ) : (
            <ChevronRight className="w-4 h-4" />
          )}
          <span>{title}</span>
          {badge !== undefined && badge > 0 && (
            <span className="px-1.5 py-0.5 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded normal-case">
              {badge}
            </span>
          )}
        </div>
      </button>
      {isExpanded && (
        <div className="bg-gray-50 dark:bg-gray-800/50 rounded-lg p-3">{children}</div>
      )}
    </div>
  );
};

// Child agent row component - clickable to switch view
const ChildAgentRow: React.FC<{
  childAgent: AgentState;
  onSelect?: (agentId: string) => void;
}> = ({ childAgent, onSelect }) => {
  const isResolver = !childAgent.task_id.includes('repair');
  const isRepair = childAgent.task_id.includes('repair');
  const isRunning = childAgent.status === 'running' || childAgent.status === 'starting';

  const statusColors: Record<string, string> = {
    starting: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300',
    running: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300',
    completed: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300',
    failed: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300',
    timed_out: 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300',
    cancelled: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
  };

  return (
    <div
      onClick={() => onSelect?.(childAgent.id)}
      className={`
        flex items-center justify-between p-2 rounded-lg border transition-all
        ${onSelect ? 'cursor-pointer hover:border-blue-400 dark:hover:border-blue-500' : ''}
        ${isRunning
          ? 'border-amber-300 dark:border-amber-600 bg-amber-50 dark:bg-amber-900/20'
          : 'border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800'
        }
      `}
    >
      <div className="flex items-center gap-2">
        <div className={`p-1 rounded ${isRunning ? 'bg-amber-100 dark:bg-amber-800' : 'bg-gray-100 dark:bg-gray-700'}`}>
          {isResolver ? (
            <GitMerge className={`w-4 h-4 ${isRunning ? 'text-amber-600 dark:text-amber-400' : 'text-purple-600 dark:text-purple-400'}`} />
          ) : isRepair ? (
            <RefreshCw className={`w-4 h-4 ${isRunning ? 'text-amber-600 dark:text-amber-400 animate-spin' : 'text-orange-600 dark:text-orange-400'}`} />
          ) : (
            <Activity className={`w-4 h-4 ${isRunning ? 'text-amber-600 dark:text-amber-400' : 'text-gray-600 dark:text-gray-400'}`} />
          )}
        </div>
        <div>
          <div className="text-sm font-medium text-gray-800 dark:text-gray-200">
            {isResolver ? 'Conflict Resolver' : isRepair ? 'Repair Agent' : 'Child Agent'}
          </div>
          <div className="text-xs font-mono text-gray-500 dark:text-gray-400">{childAgent.id}</div>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <span className={`text-xs px-2 py-0.5 rounded ${statusColors[childAgent.status] || 'bg-gray-100 text-gray-600'}`}>
          {isRunning ? (isResolver ? 'Resolving' : 'Running') : childAgent.status}
        </span>
        {onSelect && (
          <ExternalLink className="w-4 h-4 text-gray-400 dark:text-gray-500" />
        )}
      </div>
    </div>
  );
};

export const AgentDetail: React.FC<AgentDetailProps> = ({ agent, onSelectAgent }) => {
  const agents = useStateStore((state) => state.agents);
  const hasTokenUsage = agent.token_usage && agent.token_usage.total_tokens > 0;
  const hasMergeStatus = agent.merge_status && agent.merge_status !== 'pending';

  // Get child agents (resolvers, repair agents)
  const childAgents = Object.values(agents).filter(
    a => a.parent_agent_id === agent.id
  ).sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime());

  // Get parent agent if this is a child
  const parentAgent = agent.parent_agent_id ? agents[agent.parent_agent_id] : null;

  return (
    <div className="h-full overflow-y-auto p-4 bg-gray-50 dark:bg-gray-900">
      {/* Task Information */}
      <Section title="Task">
        <div className="space-y-3">
          {/* Full title */}
          <div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Title</div>
            <div className="text-sm text-gray-800 dark:text-gray-200 font-medium">{agent.task_title}</div>
          </div>

          {/* Description */}
          {agent.task_description && (
            <div>
              <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Description</div>
              <div className="text-sm text-gray-700 dark:text-gray-300 whitespace-pre-wrap bg-white dark:bg-gray-800 rounded p-2 border border-gray-200 dark:border-gray-700">
                {agent.task_description}
              </div>
            </div>
          )}

          {/* Task ID */}
          <DetailRow icon={<Hash className="w-4 h-4" />} label="Task ID" value={agent.task_id} monospace />
        </div>
      </Section>

      {/* Agent Status */}
      <Section title="Status">
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-xs text-gray-500 dark:text-gray-400">Current Status</span>
            <StatusBadge status={agent.status} />
          </div>

          {agent.error && (
            <div className="mt-2 p-2 bg-red-50 dark:bg-red-900/20 rounded border border-red-200 dark:border-red-800">
              <div className="flex items-start gap-2">
                <AlertCircle className="w-4 h-4 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
                <div className="text-sm text-red-700 dark:text-red-300">{agent.error}</div>
              </div>
            </div>
          )}

          {agent.result_message && (
            <div className="mt-2 p-2 bg-green-50 dark:bg-green-900/20 rounded border border-green-200 dark:border-green-800">
              <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Result</div>
              <div className="text-sm text-green-700 dark:text-green-300 whitespace-pre-wrap">{agent.result_message}</div>
            </div>
          )}
        </div>
      </Section>

      {/* Retry Information - shown when this is a retry attempt */}
      {agent.attempt !== undefined && agent.attempt > 1 && (
        <Section title="Retry Information">
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <RefreshCw className="w-4 h-4 text-orange-500" />
              <span className="text-sm text-gray-800 dark:text-gray-200">
                {agent.max_retries === -1
                  ? `Retry ${agent.attempt - 1} (infinite retries enabled)`
                  : `Attempt ${agent.attempt} of ${(agent.max_retries ?? 0) + 1}`}
              </span>
            </div>

            {/* Show previous failure reason if available */}
            {(agent.merge_error || agent.validation_error) && (
              <div className="mt-2 p-2 bg-orange-50 dark:bg-orange-900/20 rounded border border-orange-200 dark:border-orange-800">
                <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Previous Failure Reason</div>
                <div className="text-sm text-orange-700 dark:text-orange-300 whitespace-pre-wrap">
                  {agent.merge_error || agent.validation_error}
                </div>
              </div>
            )}

            {/* Show last repair output if available */}
            {agent.last_repair_output && (
              <div className="mt-2 p-2 bg-yellow-50 dark:bg-yellow-900/20 rounded border border-yellow-200 dark:border-yellow-800">
                <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Last Repair Output</div>
                <div className="text-sm text-yellow-700 dark:text-yellow-300 whitespace-pre-wrap font-mono text-xs">
                  {agent.last_repair_output}
                </div>
              </div>
            )}
          </div>
        </Section>
      )}

      {/* Timing */}
      <Section title="Timing">
        <div className="grid grid-cols-2 gap-4">
          <DetailRow icon={<Clock className="w-4 h-4" />} label="Started" value={formatTimestamp(agent.start_time)} />
          {agent.end_time && (
            <DetailRow icon={<Clock className="w-4 h-4" />} label="Ended" value={formatTimestamp(agent.end_time)} />
          )}
          <DetailRow
            icon={<Timer className="w-4 h-4" />}
            label="Duration"
            value={agent.duration > 0 ? formatDuration(agent.duration) : 'Running...'}
          />
        </div>
      </Section>

      {/* Token Usage */}
      {hasTokenUsage && (
        <Section title="Token Usage">
          <div className="grid grid-cols-2 gap-4">
            <DetailRow
              icon={<Coins className="w-4 h-4" />}
              label="Input Tokens"
              value={formatTokens(agent.token_usage.input_tokens)}
            />
            <DetailRow
              icon={<Coins className="w-4 h-4" />}
              label="Output Tokens"
              value={formatTokens(agent.token_usage.output_tokens)}
            />
            {agent.token_usage.cache_creation_input_tokens !== undefined && agent.token_usage.cache_creation_input_tokens > 0 && (
              <DetailRow
                icon={<Coins className="w-4 h-4" />}
                label="Cache Creation"
                value={formatTokens(agent.token_usage.cache_creation_input_tokens)}
              />
            )}
            {agent.token_usage.cache_read_input_tokens !== undefined && agent.token_usage.cache_read_input_tokens > 0 && (
              <DetailRow
                icon={<Coins className="w-4 h-4" />}
                label="Cache Read"
                value={formatTokens(agent.token_usage.cache_read_input_tokens)}
              />
            )}
            <DetailRow
              icon={<Coins className="w-4 h-4" />}
              label="Total Tokens"
              value={formatTokens(agent.token_usage.total_tokens)}
            />
            <DetailRow
              icon={<DollarSign className="w-4 h-4" />}
              label="Cost"
              value={formatCost(agent.token_usage.cost_usd)}
            />
          </div>
        </Section>
      )}

      {/* Work Summary */}
      <Section title="Work Summary">
        <DetailRow
          icon={<FileText className="w-4 h-4" />}
          label="Files Changed"
          value={agent.changes.toString()}
        />
      </Section>

      {/* Commits - Collapsible list */}
      <CollapsibleSection
        title="Commits"
        badge={agent.git_commits?.length || agent.commits || 0}
        defaultExpanded={false}
      >
        <CommitList commits={agent.git_commits || []} />
      </CollapsibleSection>

      {/* Merge Status */}
      {hasMergeStatus && (
        <Section title="Merge Status">
          <MergeStatusBadge
            status={agent.merge_status!}
            {...(agent.merge_queue_pos !== undefined && { queuePos: agent.merge_queue_pos })}
            {...(agent.merge_error !== undefined && { error: agent.merge_error })}
          />
        </Section>
      )}

      {/* Related Agents - show parent and children with click-to-view */}
      {(childAgents.length > 0 || parentAgent) && (
        <Section title="Related Agents">
          <div className="space-y-2">
            {/* Parent agent link */}
            {parentAgent && (
              <div
                onClick={() => onSelectAgent?.(parentAgent.id)}
                className={`
                  flex items-center justify-between p-2 rounded-lg border transition-all
                  ${onSelectAgent ? 'cursor-pointer hover:border-blue-400 dark:hover:border-blue-500' : ''}
                  border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800
                `}
              >
                <div className="flex items-center gap-2">
                  <div className="p-1 rounded bg-gray-100 dark:bg-gray-700">
                    <Users className="w-4 h-4 text-gray-600 dark:text-gray-400" />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-gray-800 dark:text-gray-200">Parent Agent</div>
                    <div className="text-xs font-mono text-gray-500 dark:text-gray-400">{parentAgent.id}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-gray-500 dark:text-gray-400">{parentAgent.task_title}</span>
                  {onSelectAgent && (
                    <ExternalLink className="w-4 h-4 text-gray-400 dark:text-gray-500" />
                  )}
                </div>
              </div>
            )}

            {/* Child agents */}
            {childAgents.map(child => (
              <ChildAgentRow
                key={child.id}
                childAgent={child}
                {...(onSelectAgent && { onSelect: onSelectAgent })}
              />
            ))}
          </div>
        </Section>
      )}

      {/* Agent IDs */}
      <Section title="Identifiers">
        <div className="space-y-2">
          <DetailRow icon={<Hash className="w-4 h-4" />} label="Agent ID" value={agent.id} monospace />
          {agent.run_id && (
            <DetailRow icon={<Hash className="w-4 h-4" />} label="Run ID" value={agent.run_id} monospace />
          )}
        </div>
      </Section>
    </div>
  );
};
