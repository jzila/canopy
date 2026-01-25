import React, { useState } from 'react';
import { ChevronDown, ChevronRight, Check, X, Clock, Loader2, Wrench, GitMerge, Play } from 'lucide-react';
import type { AgentState, ValidationStep } from '../../stores/stateStore';

interface AgentChainTimelineProps {
  agent: AgentState;
  childAgents?: AgentState[];
  priorAttempts?: AgentState[];  // Agents with same task_id but earlier attempts
  onSelectAgent?: (agentId: string) => void;
}

const formatDuration = (ms: number): string => {
  const seconds = Math.floor(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;

  if (minutes > 0) {
    return `${minutes}m ${remainingSeconds}s`;
  }
  return `${seconds}s`;
};

const getStatusIcon = (status: string) => {
  switch (status) {
    case 'completed':
    case 'success':
    case 'passed':
    case 'merged':
      return <Check className="w-3.5 h-3.5 text-green-500" />;
    case 'failed':
      return <X className="w-3.5 h-3.5 text-red-500" />;
    case 'running':
    case 'pending':
    case 'acquiring':
    case 'merging':
      return <Loader2 className="w-3.5 h-3.5 text-blue-500 animate-spin" />;
    case 'repairing':
      return <Wrench className="w-3.5 h-3.5 text-orange-500 animate-pulse" />;
    case 'skipped':
      return <Clock className="w-3.5 h-3.5 text-gray-400" />;
    default:
      return <Clock className="w-3.5 h-3.5 text-gray-400" />;
  }
};

const getStatusColor = (status: string) => {
  switch (status) {
    case 'completed':
    case 'success':
    case 'passed':
    case 'merged':
      return 'text-green-600 dark:text-green-400';
    case 'failed':
      return 'text-red-600 dark:text-red-400';
    case 'running':
    case 'pending':
    case 'acquiring':
    case 'merging':
      return 'text-blue-600 dark:text-blue-400';
    case 'repairing':
      return 'text-orange-600 dark:text-orange-400';
    case 'skipped':
      return 'text-gray-500 dark:text-gray-400';
    default:
      return 'text-gray-500 dark:text-gray-400';
  }
};

interface TimelineItemProps {
  icon: React.ReactNode;
  title: string;
  status: string;
  duration?: number | undefined;
  output?: string | undefined;
  isLast?: boolean;
  children?: React.ReactNode;
  onClick?: (() => void) | undefined;
  isClickable?: boolean;
  attempt?: number | undefined;
}

// Format status with optional attempt number: "completed" -> "Completed (2)"
const formatStatusWithAttempt = (status: string, attempt?: number): string => {
  // Capitalize first letter of status
  const capitalized = status.charAt(0).toUpperCase() + status.slice(1);
  if (attempt !== undefined && attempt >= 1) {
    return `${capitalized} (${attempt})`;
  }
  return capitalized;
};

const TimelineItem: React.FC<TimelineItemProps> = ({
  icon,
  title,
  status,
  duration,
  output,
  isLast = false,
  children,
  onClick,
  isClickable = false,
  attempt,
}) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const hasExpandableContent = output || children;
  const displayStatus = formatStatusWithAttempt(status, attempt);

  const handleClick = () => {
    if (isClickable && onClick) {
      onClick();
    } else if (hasExpandableContent) {
      setIsExpanded(!isExpanded);
    }
  };

  return (
    <div className="relative">
      {/* Vertical connector line */}
      {!isLast && (
        <div className="absolute left-[9px] top-6 bottom-0 w-0.5 bg-gray-200 dark:bg-gray-700" />
      )}

      <div className="flex items-start gap-3">
        {/* Status indicator */}
        <div className={`flex-shrink-0 w-5 h-5 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center border border-gray-200 dark:border-gray-700 ${isClickable ? 'ring-2 ring-amber-400 dark:ring-amber-500' : ''}`}>
          {icon}
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0 pb-4">
          <div
            className={`flex items-center gap-2 ${
              isClickable
                ? 'cursor-pointer hover:bg-amber-50 dark:hover:bg-amber-900/30 -mx-1 px-1 py-0.5 rounded transition-colors'
                : hasExpandableContent
                  ? 'cursor-pointer'
                  : ''
            }`}
            onClick={handleClick}
            title={isClickable ? 'Click to view logs' : undefined}
          >
            {hasExpandableContent && !isClickable && (
              isExpanded
                ? <ChevronDown className="w-3 h-3 text-gray-400" />
                : <ChevronRight className="w-3 h-3 text-gray-400" />
            )}
            <span className={`text-xs font-medium ${isClickable ? 'text-amber-700 dark:text-amber-300' : 'text-gray-700 dark:text-gray-300'}`}>{title}</span>
            <span className={`text-xs ${getStatusColor(status)}`}>{displayStatus}</span>
            {duration !== undefined && (
              <span className="text-xs text-gray-400 font-mono tabular-nums ml-auto">
                {formatDuration(duration)}
              </span>
            )}
            {isClickable && (
              <span className="text-xs text-amber-500 dark:text-amber-400 ml-1">→</span>
            )}
          </div>

          {isExpanded && !isClickable && (
            <div className="mt-2 text-xs">
              {output && (
                <pre className="p-2 bg-gray-100 dark:bg-gray-900 rounded text-gray-600 dark:text-gray-400 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap">
                  {output}
                </pre>
              )}
              {children}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export const AgentChainTimeline: React.FC<AgentChainTimelineProps> = ({
  agent,
  childAgents = [],
  priorAttempts = [],
  onSelectAgent,
}) => {
  // Build the agent chain from agent state
  const items: Array<{
    type: 'agent' | 'resolver' | 'validation' | 'repair';
    title: string;
    status: string;
    duration?: number | undefined;
    output?: string | undefined;
    steps?: ValidationStep[] | undefined;
    attempt?: number | undefined;
    agentId?: string | undefined;
  }> = [];

  // Sort prior attempts by attempt number ascending
  const sortedPriorAttempts = [...priorAttempts].sort((a, b) =>
    (a.attempt ?? 1) - (b.attempt ?? 1)
  );

  // 1. Prior attempt agents (failed attempts before the current one)
  for (const priorAgent of sortedPriorAttempts) {
    const attemptNum = priorAgent.attempt ?? 1;
    items.push({
      type: 'agent',
      title: 'Worker',
      status: priorAgent.status,
      duration: priorAgent.duration * 1000,
      output: priorAgent.error || undefined,
      agentId: priorAgent.id,
      attempt: attemptNum,
    });
  }

  // 2. Current implementor agent
  const hasRetries = priorAttempts.length > 0 || (agent.attempt !== undefined && agent.attempt > 1);
  const currentAttempt = agent.attempt ?? (priorAttempts.length + 1);
  items.push({
    type: 'agent',
    title: hasRetries ? 'Worker' : 'Implementor',
    status: agent.status,
    duration: agent.duration * 1000, // Convert seconds to ms
    agentId: agent.id,
    attempt: hasRetries ? currentAttempt : undefined,
  });

  // 3. Check for resolver (child agent that resolves conflicts)
  const resolverAgents = childAgents.filter(child =>
    child.parent_agent_id === agent.id && !child.task_id.includes('repair')
  );

  for (const resolver of resolverAgents) {
    items.push({
      type: 'resolver',
      title: 'Resolver',
      status: resolver.status === 'completed' ? 'resolved' : resolver.status,
      duration: resolver.duration * 1000,
      output: resolver.error || undefined,
      agentId: resolver.id,
    });
  }

  // 4. Validation (if applicable)
  if (agent.validation_status) {
    items.push({
      type: 'validation',
      title: 'Validation',
      status: agent.validation_status,
      duration: agent.validation_duration_ms,
      output: agent.validation_error,
      steps: agent.validation_steps,
    });
  }

  // 5. Repair agents (if any) - only match agents with 'repair' in task_id
  const repairAgents = childAgents.filter(child =>
    child.task_id.includes('repair')
  ).sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime());

  let repairAttempt = 0;
  for (const repair of repairAgents) {
    repairAttempt++;
    items.push({
      type: 'repair',
      title: `Repair #${repairAttempt}`,
      status: repair.status === 'completed' ? 'fixed' : repair.status,
      duration: repair.duration * 1000,
      output: repair.error || agent.last_repair_output,
      attempt: repairAttempt,
      agentId: repair.id,
    });

    // If repair was successful, add a validation pass after it
    if (repair.status === 'completed' && agent.validation_status === 'passed') {
      items.push({
        type: 'validation',
        title: 'Validation',
        status: 'passed',
        duration: agent.validation_duration_ms,
      });
    }
  }

  // Note: We only show repair agents when they actually exist in childAgents.
  // The repair_attempts field on the parent agent is for tracking purposes,
  // but we don't create phantom repair items from it.

  // Don't render if there's just the implementor agent with no special status
  if (items.length === 1 && !agent.merge_status && !agent.validation_status) {
    return null;
  }

  return (
    <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
      <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-2 flex items-center gap-1.5">
        <Play className="w-3 h-3" />
        Agent Chain
      </div>
      <div className="ml-1">
        {items.map((item, index) => {
          // Make resolvers, prior attempt agents, and repair agents clickable
          const isClickable = item.agentId && onSelectAgent && (
            item.type === 'resolver' ||
            item.type === 'repair' ||
            (item.type === 'agent' && item.agentId !== agent.id) // Prior attempts
          );
          return (
          <TimelineItem
            key={`${item.type}-${index}`}
            icon={
              item.type === 'agent' ? <Play className="w-3 h-3" /> :
              item.type === 'resolver' ? <GitMerge className="w-3 h-3 text-amber-500" /> :
              item.type === 'validation' ? getStatusIcon(item.status) :
              <Wrench className="w-3 h-3" />
            }
            title={item.title}
            status={item.status}
            duration={item.duration}
            output={item.output}
            isLast={index === items.length - 1}
            isClickable={Boolean(isClickable)}
            onClick={isClickable ? () => onSelectAgent(item.agentId!) : undefined}
            attempt={item.attempt}
          >
            {/* Render validation steps if available */}
            {item.type === 'validation' && item.steps && item.steps.length > 0 && (
              <div className="mt-2 space-y-1">
                {item.steps.map((step, stepIndex) => (
                  <div key={stepIndex} className="flex items-center gap-2 text-xs">
                    {getStatusIcon(step.status)}
                    <span className="text-gray-600 dark:text-gray-400">{step.name}</span>
                    <span className={getStatusColor(step.status)}>{step.status}</span>
                    {step.duration_ms > 0 && (
                      <span className="text-gray-400 font-mono tabular-nums ml-auto">
                        {formatDuration(step.duration_ms)}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </TimelineItem>
        );
        })}
      </div>
    </div>
  );
};

export default AgentChainTimeline;
