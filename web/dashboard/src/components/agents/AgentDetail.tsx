import React from 'react';
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
} from 'lucide-react';
import type { AgentState, MergeStatus } from '../../stores/stateStore';

interface AgentDetailProps {
  agent: AgentState;
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
  const statusConfig: Record<MergeStatus, { color: string; label: string }> = {
    pending: { color: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300', label: 'Pending' },
    acquiring: { color: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300', label: 'Acquiring Lock' },
    merging: { color: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300', label: 'Merging' },
    resolving: { color: 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300', label: 'Resolving Conflicts' },
    merged: { color: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300', label: 'Merged' },
    failed: { color: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300', label: 'Merge Failed' },
    skipped: { color: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300', label: 'No Changes' },
  };

  const config = statusConfig[status];
  const isSkipped = status === 'skipped';
  return (
    <div className="space-y-1">
      <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-sm font-medium ${config.color}`}>
        <GitMerge className="w-4 h-4" />
        {config.label}
        {queuePos !== undefined && queuePos > 0 && ` (Queue: #${queuePos})`}
      </span>
      {error && (
        <div className={`text-xs flex items-start gap-1 mt-1 ${isSkipped ? 'text-gray-500 dark:text-gray-400' : 'text-red-600 dark:text-red-400'}`}>
          {!isSkipped && <AlertCircle className="w-3 h-3 mt-0.5 flex-shrink-0" />}
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

export const AgentDetail: React.FC<AgentDetailProps> = ({ agent }) => {
  const hasTokenUsage = agent.token_usage && agent.token_usage.total_tokens > 0;
  const hasMergeStatus = agent.merge_status && agent.merge_status !== 'pending';

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
        <div className="grid grid-cols-2 gap-4">
          <DetailRow
            icon={<FileText className="w-4 h-4" />}
            label="Files Changed"
            value={agent.changes.toString()}
          />
          <DetailRow
            icon={<GitMerge className="w-4 h-4" />}
            label="Commits"
            value={(agent.git_commits?.length || agent.commits || 0).toString()}
          />
        </div>
      </Section>

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

      {/* Agent IDs */}
      <Section title="Identifiers">
        <div className="space-y-2">
          <DetailRow icon={<Hash className="w-4 h-4" />} label="Agent ID" value={agent.id} monospace />
          {agent.run_id && (
            <DetailRow icon={<Hash className="w-4 h-4" />} label="Run ID" value={agent.run_id} monospace />
          )}
          {agent.parent_agent_id && (
            <DetailRow icon={<Hash className="w-4 h-4" />} label="Parent Agent" value={agent.parent_agent_id} monospace />
          )}
          {agent.child_agent_ids && agent.child_agent_ids.length > 0 && (
            <DetailRow
              icon={<Hash className="w-4 h-4" />}
              label="Child Agents"
              value={agent.child_agent_ids.join(', ')}
              monospace
            />
          )}
        </div>
      </Section>
    </div>
  );
};
