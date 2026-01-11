import React, { useEffect, useState } from 'react';
import { Clock, Zap, DollarSign, XCircle } from 'lucide-react';
import type { AgentState } from '../../stores/stateStore';
import { killAgent } from '../../api/client';

interface AgentCardProps {
  agent: AgentState;
  onSelect: (agentId: string) => void;
  isSelected: boolean;
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
}) => {
  const [elapsedTime, setElapsedTime] = useState<string>(
    formatElapsedTime(agent.start_time, agent.end_time)
  );
  const [isKilling, setIsKilling] = useState(false);

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

  const handleCardClick = () => {
    onSelect(agent.id);
  };

  const isRunning = agent.status === 'running' || agent.status === 'starting';
  const statusColor = STATUS_COLORS[agent.status] || 'bg-gray-500';

  return (
    <div
      onClick={handleCardClick}
      className={`
        p-4 rounded-lg border-2 transition-all cursor-pointer
        ${isSelected
          ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/30 shadow-lg'
          : 'border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 hover:border-gray-300 dark:hover:border-gray-600 hover:shadow-md'
        }
      `}
    >
      <div className="flex items-start justify-between mb-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <code className="text-sm font-mono text-gray-500 dark:text-gray-400">
              {agent.task_id}
            </code>
            <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${statusColor}`}>
              {agent.status}
            </span>
          </div>
          <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">
            {agent.task_title}
          </h3>
        </div>

        {isRunning && (
          <button
            onClick={handleKill}
            disabled={isKilling}
            className={`
              ml-2 p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/30 transition-colors
              ${isKilling ? 'opacity-50 cursor-not-allowed' : ''}
            `}
            title="Kill agent"
          >
            <XCircle className="w-5 h-5 text-red-600 dark:text-red-400" />
          </button>
        )}
      </div>

      <div className="grid grid-cols-3 gap-3 text-xs">
        <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
          <Clock className="w-4 h-4" />
          <span>{elapsedTime}</span>
        </div>

        <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
          <Zap className="w-4 h-4" />
          <span>{formatTokenCount(agent.token_usage.total_tokens)}</span>
        </div>

        <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
          <DollarSign className="w-4 h-4" />
          <span>{formatCost(agent.token_usage.cost_usd)}</span>
        </div>
      </div>

      {agent.error && (
        <div className="mt-3 text-xs text-red-600 dark:text-red-400 truncate" title={agent.error}>
          Error: {agent.error}
        </div>
      )}
    </div>
  );
};
