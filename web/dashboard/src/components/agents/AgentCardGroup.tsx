import React, { useState, useMemo } from 'react';
import { ChevronDown, ChevronRight, GitMerge, ExternalLink } from 'lucide-react';
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
  const [isExpanded, setIsExpanded] = useState(true);

  const hasChildren = childAgents.length > 0;

  // Check if any child is currently active (running or starting)
  const hasActiveChild = useMemo(() => {
    return childAgents.some(
      (child) => child.status === 'running' || child.status === 'starting'
    );
  }, [childAgents]);

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
    <div className="relative">
      {/* Parent card with resolving indicator */}
      <div className="relative">
        <AgentCard
          agent={parentAgent}
          onSelect={onSelect}
          isSelected={parentAgent.id === selectedAgentId}
          {...(onArchiveToggle && { onArchiveToggle })}
        />

        {/* Resolving badge overlay */}
        {isResolving && (
          <div className="absolute -top-2 -right-2 flex items-center gap-1 px-2 py-1 bg-amber-500 text-white text-xs font-medium rounded-full shadow-lg animate-pulse">
            <GitMerge className="w-3 h-3" />
            Resolving
          </div>
        )}
      </div>

      {/* Child cards toggle and container */}
      <div className="mt-1 ml-4">
        {/* Toggle button */}
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="flex items-center gap-1 px-2 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          {isExpanded ? (
            <ChevronDown className="w-3 h-3" />
          ) : (
            <ChevronRight className="w-3 h-3" />
          )}
          <GitMerge className="w-3 h-3" />
          <span>
            {childAgents.length} resolver{childAgents.length !== 1 ? 's' : ''}
          </span>
        </button>

        {/* Stacked child cards */}
        {isExpanded && (
          <div className="relative mt-1 space-y-2">
            {/* Connecting line */}
            <div className="absolute left-0 top-0 bottom-2 w-px bg-gradient-to-b from-amber-400 to-transparent dark:from-amber-500" />

            {childAgents.map((child, index) => (
              <div
                key={child.id}
                className="relative pl-4"
                style={{
                  // Slight offset for stacked effect
                  marginLeft: `${Math.min(index * 4, 12)}px`,
                }}
              >
                {/* Connecting horizontal line */}
                <div className="absolute left-0 top-1/2 w-4 h-px bg-amber-400 dark:bg-amber-500" />

                {/* Child card with resolver styling */}
                <div
                  className={`
                    relative rounded-lg border-2 transition-all
                    ${child.status === 'running' || child.status === 'starting'
                      ? 'border-amber-400 dark:border-amber-500 bg-amber-50 dark:bg-amber-900/20'
                      : child.status === 'completed'
                        ? 'border-green-300 dark:border-green-700 bg-green-50 dark:bg-green-900/20'
                        : child.status === 'failed'
                          ? 'border-red-300 dark:border-red-700 bg-red-50 dark:bg-red-900/20'
                          : 'border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50'
                    }
                  `}
                >
                  <ResolverCard
                    agent={child}
                    onSelect={onSelect}
                    isSelected={child.id === selectedAgentId}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

// Compact resolver card component
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

const ResolverCard: React.FC<ResolverCardProps> = ({
  agent,
  onSelect,
  isSelected,
}) => {
  const statusColor = STATUS_COLORS[agent.status] || 'bg-gray-500';
  const setHighlightedTask = useStateStore((state) => state.setHighlightedTask);
  const tasks = useStateStore((state) => state.tasks);
  const taskExistsInBeads = Boolean(tasks[agent.task_id]);

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
        p-3 cursor-pointer transition-all
        ${isSelected ? 'ring-2 ring-blue-500 ring-inset' : ''}
      `}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
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
          <span
            className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium text-white ${statusColor}`}
          >
            {agent.status}
          </span>
        </div>
      </div>

      <p className="mt-1 text-xs text-gray-600 dark:text-gray-300 truncate">
        {agent.task_title}
      </p>

      {agent.error && (
        <p className="mt-1 text-xs text-red-600 dark:text-red-400 truncate">
          {agent.error}
        </p>
      )}
    </div>
  );
};
