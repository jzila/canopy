import React, { useState, useMemo } from 'react';
import { ChevronDown, ChevronUp, GitMerge, ExternalLink, Clock, Zap, DollarSign } from 'lucide-react';
import { AgentCard } from './AgentCard';
import type { AgentState } from '../../stores/stateStore';
import { useStateStore } from '../../stores/stateStore';

interface AgentCardGroupProps {
  parentAgent: AgentState;
  childAgents: AgentState[];
  onSelect: (agentId: string) => void;
  selectedAgentId: string | null;
  onArchiveToggle?: (agentId: string, archived: boolean) => void;
}

export const AgentCardGroup: React.FC<AgentCardGroupProps> = ({
  parentAgent,
  childAgents,
  onSelect,
  selectedAgentId,
  onArchiveToggle,
}) => {
  const [isExpanded, setIsExpanded] = useState(false);

  const hasChildren = childAgents.length > 0;

  // Check if any child is currently active (running or starting)
  const hasActiveChild = useMemo(() => {
    return childAgents.some(
      (child) => child.status === 'running' || child.status === 'starting'
    );
  }, [childAgents]);

  // Auto-expand when a child is actively running
  React.useEffect(() => {
    if (hasActiveChild) {
      setIsExpanded(true);
    }
  }, [hasActiveChild]);

  // Check if parent is in a "resolving" state (has active resolver child)
  const isResolving = hasActiveChild;

  if (!hasChildren) {
    // No children - render just the parent card
    return (
      <AgentCard
        agent={parentAgent}
        onSelect={onSelect}
        isSelected={parentAgent.id === selectedAgentId}
        {...(onArchiveToggle && { onArchiveToggle })}
      />
    );
  }

  return (
    <div className="relative agent-card-group">
      {/* Stacked cards container - creates the visual stack effect */}
      <div className="relative">
        {/* Background resolver cards (collapsed state - peek out from behind) */}
        {!isExpanded && childAgents.slice(0, 2).map((child, index) => (
          <div
            key={`peek-${child.id}`}
            className="absolute inset-0 rounded-lg border-2 border-amber-300 dark:border-amber-600 bg-amber-50 dark:bg-amber-900/30 transition-all duration-200"
            style={{
              transform: `translateY(${(index + 1) * 8}px) scale(${1 - (index + 1) * 0.02})`,
              zIndex: -index - 1,
              opacity: 0.8 - index * 0.2,
            }}
          />
        ))}

        {/* Parent card with resolving indicator */}
        <div className="relative z-10">
          <AgentCard
            agent={parentAgent}
            onSelect={onSelect}
            isSelected={parentAgent.id === selectedAgentId}
            {...(onArchiveToggle && { onArchiveToggle })}
          />

          {/* Resolving badge overlay */}
          {isResolving && (
            <div className="absolute -top-2 -right-2 flex items-center gap-1 px-2 py-1 bg-amber-500 text-white text-xs font-medium rounded-full shadow-lg animate-pulse z-20">
              <GitMerge className="w-3 h-3" />
              Resolving
            </div>
          )}

          {/* Resolver count badge (collapsed) */}
          {!isExpanded && (
            <div className="absolute -bottom-1 left-1/2 -translate-x-1/2 translate-y-1/2 z-20">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setIsExpanded(true);
                }}
                className="flex items-center gap-1 px-2 py-0.5 bg-amber-100 dark:bg-amber-900/50 border border-amber-300 dark:border-amber-600 text-amber-700 dark:text-amber-300 text-xs font-medium rounded-full hover:bg-amber-200 dark:hover:bg-amber-800/50 transition-colors shadow-sm"
              >
                <GitMerge className="w-3 h-3" />
                {childAgents.length} resolver{childAgents.length !== 1 ? 's' : ''}
                <ChevronDown className="w-3 h-3" />
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Expanded resolver cards - slide out below */}
      {isExpanded && (
        <div className="mt-2 space-y-2 resolver-cards-expanded">
          {childAgents.map((child, index) => (
            <div
              key={child.id}
              className="relative ml-4 animate-slide-down"
              style={{
                animationDelay: `${index * 50}ms`,
              }}
            >
              {/* Connecting line from parent to resolver */}
              <div className="absolute -left-4 top-0 bottom-0 w-4">
                {/* Vertical line */}
                <div className="absolute left-0 top-0 bottom-1/2 w-0.5 bg-gradient-to-b from-amber-400 to-amber-400 dark:from-amber-500 dark:to-amber-500" />
                {/* Horizontal line to card */}
                <div className="absolute left-0 top-1/2 w-full h-0.5 bg-amber-400 dark:bg-amber-500" />
                {/* Continuing vertical line for non-last items */}
                {index < childAgents.length - 1 && (
                  <div className="absolute left-0 top-1/2 bottom-0 w-0.5 bg-amber-400 dark:bg-amber-500" style={{ transform: 'translateY(8px)', height: 'calc(100% + 8px)' }} />
                )}
              </div>

              {/* Resolver card with special styling */}
              <ResolverCard
                agent={child}
                onSelect={onSelect}
                isSelected={child.id === selectedAgentId}
              />
            </div>
          ))}

          {/* Collapse button */}
          <button
            onClick={() => setIsExpanded(false)}
            className="ml-4 flex items-center gap-1 px-2 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
          >
            <ChevronUp className="w-3 h-3" />
            Collapse
          </button>
        </div>
      )}
    </div>
  );
};

// Compact resolver card component - styled as an extension of parent card
interface ResolverCardProps {
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

const BORDER_COLORS: Record<string, string> = {
  starting: 'border-yellow-400 dark:border-yellow-500',
  running: 'border-amber-400 dark:border-amber-500',
  completed: 'border-green-400 dark:border-green-600',
  failed: 'border-red-400 dark:border-red-600',
  timed_out: 'border-orange-400 dark:border-orange-500',
  cancelled: 'border-gray-400 dark:border-gray-600',
};

const BG_COLORS: Record<string, string> = {
  starting: 'bg-yellow-50 dark:bg-yellow-900/20',
  running: 'bg-amber-50 dark:bg-amber-900/20',
  completed: 'bg-green-50 dark:bg-green-900/20',
  failed: 'bg-red-50 dark:bg-red-900/20',
  timed_out: 'bg-orange-50 dark:bg-orange-900/20',
  cancelled: 'bg-gray-50 dark:bg-gray-800/50',
};

const formatElapsedTime = (startTime: string, endTime: string | null): string => {
  const start = new Date(startTime).getTime();
  const end = endTime ? new Date(endTime).getTime() : Date.now();
  const elapsed = Math.floor((end - start) / 1000);

  const minutes = Math.floor(elapsed / 60);
  const seconds = elapsed % 60;

  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
};

const formatTokenCount = (count: number): string => {
  if (count >= 1000) {
    return `${(count / 1000).toFixed(1)}K`;
  }
  return count.toString();
};

const formatCost = (cost: number): string => {
  if (cost < 0.01) {
    return `${(cost * 100).toFixed(1)}c`;
  }
  return `$${cost.toFixed(2)}`;
};

const ResolverCard: React.FC<ResolverCardProps> = ({
  agent,
  onSelect,
  isSelected,
}) => {
  const statusColor = STATUS_COLORS[agent.status] || 'bg-gray-500';
  const borderColor = BORDER_COLORS[agent.status] || 'border-gray-300 dark:border-gray-600';
  const bgColor = BG_COLORS[agent.status] || 'bg-gray-50 dark:bg-gray-800/50';
  const setHighlightedTask = useStateStore((state) => state.setHighlightedTask);
  const tasks = useStateStore((state) => state.tasks);
  const taskExistsInBeads = Boolean(tasks[agent.task_id]);

  const [elapsedTime, setElapsedTime] = React.useState<string>(
    formatElapsedTime(agent.start_time, agent.end_time)
  );

  // Update elapsed time for running agents
  React.useEffect(() => {
    if (agent.status === 'running' || agent.status === 'starting') {
      const interval = setInterval(() => {
        setElapsedTime(formatElapsedTime(agent.start_time, agent.end_time));
      }, 1000);
      return () => clearInterval(interval);
    } else {
      setElapsedTime(formatElapsedTime(agent.start_time, agent.end_time));
    }
  }, [agent.start_time, agent.end_time, agent.status]);

  const handleTaskIdClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (tasks[agent.task_id]) {
      setHighlightedTask(agent.task_id);
      setTimeout(() => setHighlightedTask(null), 3000);
    }
  };

  return (
    <div
      onClick={() => onSelect(agent.id)}
      className={`
        p-3 rounded-lg border-2 cursor-pointer transition-all
        ${borderColor} ${bgColor}
        ${isSelected ? 'ring-2 ring-blue-500 ring-offset-1 shadow-md' : 'hover:shadow-sm'}
      `}
    >
      {/* Header: icon, task ID, status */}
      <div className="flex items-center justify-between gap-2 mb-1">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <GitMerge className="w-4 h-4 text-amber-500 flex-shrink-0" />
          {taskExistsInBeads ? (
            <button
              onClick={handleTaskIdClick}
              className="inline-flex items-center gap-1 text-xs font-mono text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 hover:underline transition-colors truncate"
              title="Click to highlight in Beads pane"
            >
              {agent.task_id}
              <ExternalLink className="w-2.5 h-2.5 flex-shrink-0" />
            </button>
          ) : (
            <code className="text-xs font-mono text-gray-500 dark:text-gray-400 truncate">
              {agent.task_id}
            </code>
          )}
        </div>
        <span
          className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium text-white flex-shrink-0 ${statusColor}`}
        >
          {agent.status}
        </span>
      </div>

      {/* Task title */}
      <p className="text-sm text-gray-700 dark:text-gray-200 truncate mb-2">
        {agent.task_title}
      </p>

      {/* Stats row */}
      <div className="flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
        <div className="flex items-center gap-1" title="Elapsed time">
          <Clock className="w-3 h-3" />
          <span>{elapsedTime}</span>
        </div>
        <div className="flex items-center gap-1" title="Tokens">
          <Zap className="w-3 h-3" />
          <span>{formatTokenCount(agent.token_usage.total_tokens)}</span>
        </div>
        <div className="flex items-center gap-1" title="Cost">
          <DollarSign className="w-3 h-3" />
          <span>{formatCost(agent.token_usage.cost_usd)}</span>
        </div>
      </div>

      {/* Error message */}
      {agent.error && (
        <p className="mt-2 text-xs text-red-600 dark:text-red-400 truncate" title={agent.error}>
          Error: {agent.error}
        </p>
      )}
    </div>
  );
};
