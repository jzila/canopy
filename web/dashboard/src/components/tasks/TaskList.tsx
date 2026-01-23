import React, { useState, useMemo, useEffect } from 'react';
import type { TaskState } from '../../stores/stateStore';
import { archiveTask } from '../../api/client';

interface TaskListProps {
  tasks: Record<string, TaskState>;
  onSelectTask: (taskId: string) => void;
  onTaskArchived?: (taskId: string, archived: boolean) => void;
}

type StatusFilter = 'all' | 'ready' | 'running' | 'done' | 'failed' | 'paused';

const SHOW_ARCHIVED_KEY = 'canopy-show-archived';

const STATUS_COLORS: Record<string, string> = {
  ready: 'bg-gray-500',
  running: 'bg-blue-500',
  done: 'bg-green-500',
  failed: 'bg-red-500',
  blocked: 'bg-yellow-500',
  'needs-input': 'bg-orange-500',
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
  // 'paused' filter matches 'needs-input' status
  if (filter === 'paused') return task.status.toLowerCase() === 'needs-input';
  return task.status.toLowerCase() === filter;
};

export const TaskList: React.FC<TaskListProps> = ({ tasks, onSelectTask, onTaskArchived }) => {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [showArchived, setShowArchived] = useState<boolean>(() => {
    const stored = localStorage.getItem(SHOW_ARCHIVED_KEY);
    return stored === 'true';
  });

  useEffect(() => {
    localStorage.setItem(SHOW_ARCHIVED_KEY, String(showArchived));
  }, [showArchived]);

  const handleArchiveToggle = async (taskId: string, currentArchived: boolean) => {
    try {
      const newArchived = !currentArchived;
      await archiveTask(taskId, newArchived);
      onTaskArchived?.(taskId, newArchived);
    } catch (error) {
      console.error('Failed to archive task:', error);
    }
  };

  const taskList = useMemo(() => {
    return Object.values(tasks);
  }, [tasks]);

  const filteredTasks = useMemo(() => {
    return taskList
      .filter(task => {
        // Filter by archived status first
        if (!showArchived && task.archived) {
          return false;
        }
        return matchesFilter(task, statusFilter);
      })
      .sort((a, b) => {
        // Archived tasks go to the bottom
        if (a.archived !== b.archived) {
          return a.archived ? 1 : -1;
        }
        // Sort by priority first (lower number = higher priority)
        if (a.priority !== b.priority) {
          return a.priority - b.priority;
        }
        // Then by status (running > needs-input > ready > blocked > done > failed)
        const statusOrder: Record<string, number> = {
          running: 0,
          'needs-input': 1,
          ready: 2,
          blocked: 3,
          done: 4,
          failed: 5,
        };
        const aOrder = statusOrder[a.status.toLowerCase()] ?? 999;
        const bOrder = statusOrder[b.status.toLowerCase()] ?? 999;
        return aOrder - bOrder;
      });
  }, [taskList, statusFilter, showArchived]);

  const statusCounts = useMemo(() => {
    const nonArchivedTasks = showArchived ? taskList : taskList.filter(t => !t.archived);
    return {
      all: nonArchivedTasks.length,
      ready: nonArchivedTasks.filter(t => t.status.toLowerCase() === 'ready').length,
      running: nonArchivedTasks.filter(t => t.status.toLowerCase() === 'running').length,
      done: nonArchivedTasks.filter(t => t.status.toLowerCase() === 'done').length,
      failed: nonArchivedTasks.filter(t => t.status.toLowerCase() === 'failed').length,
      paused: nonArchivedTasks.filter(t => t.status.toLowerCase() === 'needs-input').length,
    };
  }, [taskList, showArchived]);

  const archivedCount = useMemo(() => {
    return taskList.filter(t => t.archived).length;
  }, [taskList]);

  const filterButtons: Array<{ value: StatusFilter; label: string }> = [
    { value: 'all', label: 'All' },
    { value: 'ready', label: 'Ready' },
    { value: 'running', label: 'Running' },
    { value: 'paused', label: 'Paused' },
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
      <div className="flex flex-wrap items-center gap-1 p-2 bg-gray-50 dark:bg-gray-700 border-b border-gray-200 dark:border-gray-700">
        {filterButtons.map(({ value, label }) => {
          const isActive = statusFilter === value;
          const count = statusCounts[value];

          return (
            <button
              key={value}
              onClick={() => setStatusFilter(value)}
              className={`
                px-2 py-1 rounded text-xs font-medium transition-colors whitespace-nowrap
                ${isActive
                  ? 'bg-blue-500 text-white shadow-sm'
                  : 'bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 border border-gray-200 dark:border-gray-600'
                }
              `}
            >
              {label}
              <span className={`ml-1 ${isActive ? 'text-blue-100' : 'text-gray-500 dark:text-gray-400'}`}>
                ({count})
              </span>
            </button>
          );
        })}

        {/* Archive Toggle */}
        <div className="ml-auto flex items-center gap-2">
          <button
            onClick={() => setShowArchived(!showArchived)}
            className={`
              px-2 py-1 rounded text-xs font-medium transition-colors whitespace-nowrap
              ${showArchived
                ? 'bg-purple-500 text-white shadow-sm'
                : 'bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 border border-gray-200 dark:border-gray-600'
              }
            `}
            title={showArchived ? 'Hide archived tasks' : 'Show archived tasks'}
          >
            {showArchived ? 'Hide' : 'Show'} Archived
            <span className={`ml-1 ${showArchived ? 'text-purple-100' : 'text-gray-500 dark:text-gray-400'}`}>
              ({archivedCount})
            </span>
          </button>
        </div>
      </div>

      {/* Task List */}
      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        {filteredTasks.length === 0 ? (
          <div className="text-center text-gray-500 dark:text-gray-400 py-8">
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
                className={`
                  p-3 rounded-lg border transition-all cursor-pointer
                  ${task.archived
                    ? 'bg-gray-100 dark:bg-gray-900 border-gray-300 dark:border-gray-600 opacity-60'
                    : 'bg-white dark:bg-gray-800 border-gray-200 dark:border-gray-700 hover:border-blue-300 dark:hover:border-blue-500 hover:shadow-md'
                  }
                `}
              >
                <div className="flex items-start gap-2 mb-2">
                  {/* Archived Badge */}
                  {task.archived && (
                    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                      Archived
                    </span>
                  )}

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
                  <code className="text-xs font-mono text-gray-500 dark:text-gray-400 ml-auto">
                    {truncateId(task.id)}
                  </code>

                  {/* Archive Button */}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleArchiveToggle(task.id, task.archived);
                    }}
                    className={`
                      p-1 rounded transition-colors
                      ${task.archived
                        ? 'text-purple-600 hover:text-purple-800 hover:bg-purple-100 dark:text-purple-400 dark:hover:text-purple-200 dark:hover:bg-purple-900'
                        : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:text-gray-500 dark:hover:text-gray-300 dark:hover:bg-gray-700'
                      }
                    `}
                    title={task.archived ? 'Unarchive task' : 'Archive task'}
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
                    </svg>
                  </button>
                </div>

                {/* Task Title */}
                <h3 className={`text-sm font-medium mb-1 ${task.archived ? 'text-gray-500 dark:text-gray-400 line-through' : 'text-gray-900 dark:text-gray-100'}`}>
                  {task.title}
                </h3>

                {/* Agent Info */}
                {task.agent_id && (
                  <div className="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                    <span className="font-medium">Agent:</span>
                    <code className="font-mono">{truncateId(task.agent_id)}</code>
                  </div>
                )}

                {/* Dependencies */}
                {task.dependencies && task.dependencies.length > 0 && (
                  <div className="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400 mt-1">
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
