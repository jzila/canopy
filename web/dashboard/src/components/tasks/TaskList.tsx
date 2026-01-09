import React, { useState, useMemo } from 'react';
import type { TaskState } from '../../stores/stateStore';

interface TaskListProps {
  tasks: Record<string, TaskState>;
  onSelectTask: (taskId: string) => void;
}

type StatusFilter = 'all' | 'ready' | 'running' | 'done' | 'failed';

const STATUS_COLORS: Record<string, string> = {
  ready: 'bg-gray-500',
  running: 'bg-blue-500',
  done: 'bg-green-500',
  failed: 'bg-red-500',
  blocked: 'bg-yellow-500',
};

const PRIORITY_COLORS: Record<number, { bg: string; text: string; label: string }> = {
  0: { bg: 'bg-red-100', text: 'text-red-800', label: 'P0' },
  1: { bg: 'bg-orange-100', text: 'text-orange-800', label: 'P1' },
  2: { bg: 'bg-yellow-100', text: 'text-yellow-800', label: 'P2' },
  3: { bg: 'bg-blue-100', text: 'text-blue-800', label: 'P3' },
  4: { bg: 'bg-gray-100', text: 'text-gray-800', label: 'P4' },
};

const DEFAULT_PRIORITY_STYLE = { bg: 'bg-gray-100', text: 'text-gray-800', label: 'P4' };

const truncateId = (id: string, length: number = 8): string => {
  return id.slice(0, length);
};

const matchesFilter = (task: TaskState, filter: StatusFilter): boolean => {
  if (filter === 'all') return true;
  return task.status.toLowerCase() === filter;
};

export const TaskList: React.FC<TaskListProps> = ({ tasks, onSelectTask }) => {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');

  const taskList = useMemo(() => {
    return Object.values(tasks);
  }, [tasks]);

  const filteredTasks = useMemo(() => {
    return taskList
      .filter(task => matchesFilter(task, statusFilter))
      .sort((a, b) => {
        // Sort by priority first (lower number = higher priority)
        if (a.priority !== b.priority) {
          return a.priority - b.priority;
        }
        // Then by status (running > ready > blocked > done > failed)
        const statusOrder: Record<string, number> = {
          running: 0,
          ready: 1,
          blocked: 2,
          done: 3,
          failed: 4,
        };
        const aOrder = statusOrder[a.status.toLowerCase()] ?? 999;
        const bOrder = statusOrder[b.status.toLowerCase()] ?? 999;
        return aOrder - bOrder;
      });
  }, [taskList, statusFilter]);

  const statusCounts = useMemo(() => {
    return {
      all: taskList.length,
      ready: taskList.filter(t => t.status.toLowerCase() === 'ready').length,
      running: taskList.filter(t => t.status.toLowerCase() === 'running').length,
      done: taskList.filter(t => t.status.toLowerCase() === 'done').length,
      failed: taskList.filter(t => t.status.toLowerCase() === 'failed').length,
    };
  }, [taskList]);

  const filterButtons: Array<{ value: StatusFilter; label: string }> = [
    { value: 'all', label: 'All' },
    { value: 'ready', label: 'Ready' },
    { value: 'running', label: 'Running' },
    { value: 'done', label: 'Done' },
    { value: 'failed', label: 'Failed' },
  ];

  const getPriorityStyle = (priority: number) => {
    return PRIORITY_COLORS[priority] || DEFAULT_PRIORITY_STYLE;
  };

  const getStatusColor = (status: string) => {
    return STATUS_COLORS[status.toLowerCase()] || 'bg-gray-500';
  };

  return (
    <div className="flex flex-col h-full">
      {/* Filter Tabs */}
      <div className="flex gap-1 p-2 bg-gray-50 border-b border-gray-200">
        {filterButtons.map(({ value, label }) => {
          const isActive = statusFilter === value;
          const count = statusCounts[value];

          return (
            <button
              key={value}
              onClick={() => setStatusFilter(value)}
              className={`
                px-3 py-1.5 rounded text-sm font-medium transition-colors
                ${isActive
                  ? 'bg-blue-500 text-white shadow-sm'
                  : 'bg-white text-gray-700 hover:bg-gray-100 border border-gray-200'
                }
              `}
            >
              {label}
              <span className={`ml-1.5 ${isActive ? 'text-blue-100' : 'text-gray-500'}`}>
                ({count})
              </span>
            </button>
          );
        })}
      </div>

      {/* Task List */}
      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        {filteredTasks.length === 0 ? (
          <div className="text-center text-gray-500 py-8">
            No tasks found
          </div>
        ) : (
          filteredTasks.map(task => {
            const priorityStyle = getPriorityStyle(task.priority);
            const statusColor = getStatusColor(task.status);

            return (
              <div
                key={task.id}
                onClick={() => onSelectTask(task.id)}
                className="p-3 bg-white rounded-lg border border-gray-200 hover:border-blue-300 hover:shadow-md transition-all cursor-pointer"
              >
                <div className="flex items-start gap-2 mb-2">
                  {/* Priority Badge */}
                  <span
                    className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-bold ${priorityStyle.bg} ${priorityStyle.text}`}
                  >
                    {priorityStyle.label}
                  </span>

                  {/* Status Badge */}
                  <span
                    className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${statusColor}`}
                  >
                    {task.status}
                  </span>

                  {/* Task ID */}
                  <code className="text-xs font-mono text-gray-500 ml-auto">
                    {truncateId(task.id)}
                  </code>
                </div>

                {/* Task Title */}
                <h3 className="text-sm font-medium text-gray-900 mb-1">
                  {task.title}
                </h3>

                {/* Agent Info */}
                {task.agent_id && (
                  <div className="flex items-center gap-2 text-xs text-gray-600">
                    <span className="font-medium">Agent:</span>
                    <code className="font-mono">{truncateId(task.agent_id)}</code>
                  </div>
                )}

                {/* Dependencies */}
                {task.dependencies && task.dependencies.length > 0 && (
                  <div className="flex items-center gap-2 text-xs text-gray-600 mt-1">
                    <span className="font-medium">Deps:</span>
                    <code className="font-mono">
                      {task.dependencies.map(d => truncateId(d)).join(', ')}
                    </code>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
