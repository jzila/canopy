import React, { useMemo, useState, useEffect, useRef, useCallback } from 'react';
import { Circle, Clock, AlertCircle, Loader2, ChevronRight, ChevronLeft, ChevronDown, GitBranch, CheckCircle2, Archive, GripVertical, ArrowUpDown } from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';
import type { TaskState } from '../../stores/stateStore';
import { useMergeQueue } from '../../hooks/useMergeQueue';
import { MergeQueueStatus } from '../merge';

interface BeadsPaneProps {
  isExpanded: boolean;
  onToggle: () => void;
  onTaskClick?: (taskId: string) => void;
  showCompleted?: boolean;
  onToggleShowCompleted?: () => void;
  /** Width of the pane in pixels (when expanded) */
  width?: number;
  /** Whether the pane is currently being resized */
  isResizing?: boolean;
  /** Handler to start resizing (attach to mousedown on resize handle) */
  onResizeStart?: (e: React.MouseEvent) => void;
  /** Currently selected bead ID for filtering agents */
  selectedBeadId?: string | null;
  /** Callback when a bead is selected/deselected */
  onBeadSelect?: (beadId: string | null) => void;
}

type ViewMode = 'hierarchy' | 'flat';
type SortMode = 'priority' | 'updated';

interface TreeNode {
  task: TaskState;
  children: TreeNode[];
  depth: number;
}

const STATUS_CONFIG: Record<string, { icon: React.ReactNode; bg: string; text: string; label: string }> = {
  ready: {
    icon: <Circle className="w-3 h-3" />,
    bg: 'bg-gray-100 dark:bg-gray-700',
    text: 'text-gray-600 dark:text-gray-400',
    label: 'Ready',
  },
  open: {
    icon: <Circle className="w-3 h-3" />,
    bg: 'bg-gray-100 dark:bg-gray-700',
    text: 'text-gray-600 dark:text-gray-400',
    label: 'Open',
  },
  running: {
    icon: <Loader2 className="w-3 h-3 animate-spin" />,
    bg: 'bg-blue-100 dark:bg-blue-900/50',
    text: 'text-blue-600 dark:text-blue-400',
    label: 'Running',
  },
  in_progress: {
    icon: <Loader2 className="w-3 h-3 animate-spin" />,
    bg: 'bg-blue-100 dark:bg-blue-900/50',
    text: 'text-blue-600 dark:text-blue-400',
    label: 'In Progress',
  },
  blocked: {
    icon: <Clock className="w-3 h-3" />,
    bg: 'bg-yellow-100 dark:bg-yellow-900/50',
    text: 'text-yellow-600 dark:text-yellow-400',
    label: 'Blocked',
  },
  failed: {
    icon: <AlertCircle className="w-3 h-3" />,
    bg: 'bg-red-100 dark:bg-red-900/50',
    text: 'text-red-600 dark:text-red-400',
    label: 'Failed',
  },
  done: {
    icon: <CheckCircle2 className="w-3 h-3" />,
    bg: 'bg-green-100 dark:bg-green-900/50',
    text: 'text-green-600 dark:text-green-400',
    label: 'Done',
  },
  completed: {
    icon: <CheckCircle2 className="w-3 h-3" />,
    bg: 'bg-green-100 dark:bg-green-900/50',
    text: 'text-green-600 dark:text-green-400',
    label: 'Completed',
  },
};

const DEFAULT_STATUS_CONFIG = {
  icon: <Circle className="w-3 h-3" />,
  bg: 'bg-gray-100 dark:bg-gray-700',
  text: 'text-gray-600 dark:text-gray-400',
  label: 'Unknown',
};

const PRIORITY_COLORS: Record<number, { bg: string; text: string; label: string }> = {
  0: { bg: 'bg-red-100 dark:bg-red-900/50', text: 'text-red-700 dark:text-red-300', label: 'P0' },
  1: { bg: 'bg-orange-100 dark:bg-orange-900/50', text: 'text-orange-700 dark:text-orange-300', label: 'P1' },
  2: { bg: 'bg-yellow-100 dark:bg-yellow-900/50', text: 'text-yellow-700 dark:text-yellow-300', label: 'P2' },
  3: { bg: 'bg-blue-100 dark:bg-blue-900/50', text: 'text-blue-700 dark:text-blue-300', label: 'P3' },
  4: { bg: 'bg-gray-100 dark:bg-gray-700', text: 'text-gray-700 dark:text-gray-300', label: 'P4' },
};

const DEFAULT_PRIORITY_STYLE = { bg: 'bg-gray-100 dark:bg-gray-700', text: 'text-gray-700 dark:text-gray-300', label: 'P4' };

const getStatusConfig = (status: string) => {
  return STATUS_CONFIG[status.toLowerCase()] || DEFAULT_STATUS_CONFIG;
};

const getPriorityStyle = (priority: number) => {
  return PRIORITY_COLORS[priority] || DEFAULT_PRIORITY_STYLE;
};

// Build a dependency tree from flat task list
// Tasks that block others become parents, tasks with dependencies become children
const buildDependencyTree = (tasks: TaskState[]): TreeNode[] => {
  const taskMap = new Map<string, TaskState>();
  const childrenMap = new Map<string, Set<string>>(); // parent -> children (tasks blocked by parent)

  // Build maps
  tasks.forEach(task => {
    taskMap.set(task.id, task);
    if (!childrenMap.has(task.id)) {
      childrenMap.set(task.id, new Set());
    }
  });

  // Populate children (reverse of dependencies)
  // If task A depends on task B, then B is the parent of A
  tasks.forEach(task => {
    if (task.dependencies && task.dependencies.length > 0) {
      task.dependencies.forEach(depId => {
        if (taskMap.has(depId)) {
          childrenMap.get(depId)?.add(task.id);
        }
      });
    }
  });

  // Find root nodes (tasks with no dependencies, or dependencies outside our visible set)
  const rootTasks = tasks.filter(task => {
    if (!task.dependencies || task.dependencies.length === 0) return true;
    // Also consider root if all dependencies are outside our visible set (completed)
    return task.dependencies.every(depId => !taskMap.has(depId));
  });

  // Build tree recursively
  const visited = new Set<string>();

  const buildNode = (task: TaskState, depth: number): TreeNode => {
    visited.add(task.id);
    const childIds = childrenMap.get(task.id) || new Set();
    const children: TreeNode[] = [];

    childIds.forEach(childId => {
      if (!visited.has(childId)) {
        const childTask = taskMap.get(childId);
        if (childTask) {
          children.push(buildNode(childTask, depth + 1));
        }
      }
    });

    // Sort children by priority, then status
    children.sort((a, b) => {
      if (a.task.priority !== b.task.priority) {
        return a.task.priority - b.task.priority;
      }
      const statusOrder: Record<string, number> = {
        running: 0, in_progress: 0, blocked: 1, ready: 2, open: 2, failed: 3,
      };
      return (statusOrder[a.task.status.toLowerCase()] ?? 999) -
             (statusOrder[b.task.status.toLowerCase()] ?? 999);
    });

    return { task, children, depth };
  };

  // Sort roots by priority then status
  rootTasks.sort((a, b) => {
    if (a.priority !== b.priority) return a.priority - b.priority;
    const statusOrder: Record<string, number> = {
      running: 0, in_progress: 0, blocked: 1, ready: 2, open: 2, failed: 3,
    };
    return (statusOrder[a.status.toLowerCase()] ?? 999) -
           (statusOrder[b.status.toLowerCase()] ?? 999);
  });

  const tree = rootTasks.map(task => buildNode(task, 0));

  // Add any orphaned nodes (cycles or missed nodes)
  tasks.forEach(task => {
    if (!visited.has(task.id)) {
      tree.push(buildNode(task, 0));
    }
  });

  return tree;
};

const truncateId = (id: string, length: number = 11): string => {
  // For bead IDs like "canopy-abc", keep the prefix and short hash
  if (id.startsWith('canopy-') || id.startsWith('beads-')) {
    return id;
  }
  return id.length > length ? id.slice(0, length) : id;
};

export const BeadsPane: React.FC<BeadsPaneProps> = ({ isExpanded, onToggle, onTaskClick, showCompleted = false, onToggleShowCompleted, width = 320, isResizing = false, onResizeStart, selectedBeadId, onBeadSelect }) => {
  const tasks = useStateStore((state) => state.tasks);
  const highlightedTaskId = useStateStore((state) => state.highlightedTaskId);
  const [viewMode, setViewMode] = useState<ViewMode>('hierarchy');
  const [sortMode, setSortMode] = useState<SortMode>('priority');
  const [collapsedNodes, setCollapsedNodes] = useState<Set<string>>(new Set());
  const taskRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  const listContainerRef = useRef<HTMLDivElement>(null);

  // Merge queue data for compact status display
  const { mergeQueue } = useMergeQueue();

  // Transform merge queue data for MergeQueueStatus component
  const mergeQueueData = useMemo(() => {
    if (!mergeQueue) {
      return { completed: [], resolvers: [], pending: [], active_workers: [] };
    }
    return {
      completed: mergeQueue.completed,
      resolvers: mergeQueue.resolvers,
      pending: mergeQueue.pending,
      active_workers: mergeQueue.active_workers,
    };
  }, [mergeQueue]);

  // Count of completed/archived beads (for toggle button badge)
  const completedBeadsCount = useMemo(() => {
    return Object.values(tasks).filter((task: TaskState) => {
      const status = task.status.toLowerCase();
      return status === 'done' || status === 'completed' || task.archived;
    }).length;
  }, [tasks]);

  // Filter beads based on showCompleted toggle
  const filteredBeads = useMemo(() => {
    return Object.values(tasks)
      .filter((task: TaskState) => {
        const status = task.status.toLowerCase();
        const isCompleted = status === 'done' || status === 'completed';
        const isArchived = task.archived;

        if (showCompleted) {
          // Show all beads when toggle is on
          return true;
        } else {
          // Only show incomplete and non-archived beads
          return !isCompleted && !isArchived;
        }
      })
      .sort((a: TaskState, b: TaskState) => {
        // Put completed/archived at the bottom when showing all
        if (showCompleted) {
          const aCompleted = a.status.toLowerCase() === 'done' || a.status.toLowerCase() === 'completed' || a.archived;
          const bCompleted = b.status.toLowerCase() === 'done' || b.status.toLowerCase() === 'completed' || b.archived;
          if (aCompleted !== bCompleted) {
            return aCompleted ? 1 : -1;
          }
        }

        // Sort by selected mode
        if (sortMode === 'updated') {
          // Sort by last updated (most recent first)
          const aUpdated = a.updated_at ?? 0;
          const bUpdated = b.updated_at ?? 0;
          if (aUpdated !== bUpdated) {
            return bUpdated - aUpdated;  // Descending (most recent first)
          }
          // Fall back to priority for ties
          if (a.priority !== b.priority) {
            return a.priority - b.priority;
          }
        } else {
          // Sort by priority first (lower = higher priority)
          if (a.priority !== b.priority) {
            return a.priority - b.priority;
          }
        }

        // Then by status (running > blocked > ready/open > failed > done)
        const statusOrder: Record<string, number> = {
          running: 0,
          in_progress: 0,
          blocked: 1,
          ready: 2,
          open: 2,
          failed: 3,
          done: 4,
          completed: 4,
        };
        const aOrder = statusOrder[a.status.toLowerCase()] ?? 999;
        const bOrder = statusOrder[b.status.toLowerCase()] ?? 999;
        return aOrder - bOrder;
      });
  }, [tasks, showCompleted, sortMode]);

  // For backward compatibility, keep incompleteBeads as an alias
  const incompleteBeads = useMemo(() => {
    return filteredBeads.filter((task: TaskState) => {
      const status = task.status.toLowerCase();
      return status !== 'done' && status !== 'completed' && !task.archived;
    });
  }, [filteredBeads]);

  // Build dependency tree
  const dependencyTree = useMemo(() => {
    return buildDependencyTree(filteredBeads);
  }, [filteredBeads]);

  // Flatten tree for rendering (respecting collapsed state)
  const flattenedTree = useMemo(() => {
    const result: TreeNode[] = [];
    const traverse = (node: TreeNode) => {
      result.push(node);
      if (!collapsedNodes.has(node.task.id)) {
        node.children.forEach(traverse);
      }
    };
    dependencyTree.forEach(traverse);
    return result;
  }, [dependencyTree, collapsedNodes]);

  // Check if any task has dependencies (useful for showing hierarchy mode)
  const hasDependencies = useMemo(() => {
    return filteredBeads.some(task => task.dependencies && task.dependencies.length > 0);
  }, [filteredBeads]);

  // Scroll to highlighted task when it changes
  useEffect(() => {
    if (highlightedTaskId && isExpanded) {
      const taskElement = taskRefs.current.get(highlightedTaskId);
      if (taskElement && listContainerRef.current) {
        taskElement.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    }
  }, [highlightedTaskId, isExpanded]);

  const toggleNodeCollapsed = (taskId: string) => {
    setCollapsedNodes(prev => {
      const next = new Set(prev);
      if (next.has(taskId)) {
        next.delete(taskId);
      } else {
        next.add(taskId);
      }
      return next;
    });
  };

  // Handle bead click - toggle selection
  const handleBeadClick = useCallback((taskId: string, e: React.MouseEvent) => {
    // If clicking on the expand/collapse button, don't toggle selection
    if ((e.target as HTMLElement).closest('button')) {
      return;
    }
    if (onBeadSelect) {
      // Toggle: if already selected, deselect; otherwise select
      onBeadSelect(selectedBeadId === taskId ? null : taskId);
    }
  }, [onBeadSelect, selectedBeadId]);

  // Count by status
  const statusCounts = useMemo(() => {
    const counts = {
      running: 0,
      blocked: 0,
      ready: 0,
      failed: 0,
    };
    incompleteBeads.forEach((task: TaskState) => {
      const status = task.status.toLowerCase();
      if (status === 'running' || status === 'in_progress') {
        counts.running++;
      } else if (status === 'blocked') {
        counts.blocked++;
      } else if (status === 'ready' || status === 'open') {
        counts.ready++;
      } else if (status === 'failed') {
        counts.failed++;
      }
    });
    return counts;
  }, [incompleteBeads]);

  // Collapsed view - just show the toggle button with count
  if (!isExpanded) {
    return (
      <div className="h-full flex flex-col items-center py-4 px-2 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700">
        <button
          onClick={onToggle}
          className="p-2.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors group"
          title="Expand beads pane"
        >
          <ChevronRight className="w-5 h-5 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
        </button>
        {incompleteBeads.length > 0 && (
          <div className="mt-3 flex flex-col items-center gap-1.5">
            <span className="text-sm font-bold text-gray-700 dark:text-gray-300">{incompleteBeads.length}</span>
            <span className="text-2xs text-gray-500 dark:text-gray-400 writing-mode-vertical" style={{ writingMode: 'vertical-rl', textOrientation: 'mixed' }}>
              beads
            </span>
          </div>
        )}
        {/* Status indicators when collapsed */}
        <div className="mt-4 flex flex-col gap-1.5">
          {statusCounts.running > 0 && (
            <div className="w-7 h-7 rounded-full bg-blue-100 dark:bg-blue-900/50 flex items-center justify-center" title={`${statusCounts.running} running`}>
              <span className="text-2xs font-bold text-blue-600 dark:text-blue-400">{statusCounts.running}</span>
            </div>
          )}
          {statusCounts.blocked > 0 && (
            <div className="w-7 h-7 rounded-full bg-yellow-100 dark:bg-yellow-900/50 flex items-center justify-center" title={`${statusCounts.blocked} blocked`}>
              <span className="text-2xs font-bold text-yellow-600 dark:text-yellow-400">{statusCounts.blocked}</span>
            </div>
          )}
          {statusCounts.ready > 0 && (
            <div className="w-7 h-7 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center" title={`${statusCounts.ready} ready`}>
              <span className="text-2xs font-bold text-gray-600 dark:text-gray-400">{statusCounts.ready}</span>
            </div>
          )}
          {statusCounts.failed > 0 && (
            <div className="w-7 h-7 rounded-full bg-red-100 dark:bg-red-900/50 flex items-center justify-center" title={`${statusCounts.failed} failed`}>
              <span className="text-2xs font-bold text-red-600 dark:text-red-400">{statusCounts.failed}</span>
            </div>
          )}
        </div>
      </div>
    );
  }

  // Expanded view
  return (
    <div className="h-full flex bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700" style={{ width }}>
      <div className="flex-1 flex flex-col min-w-0">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-center gap-3 h-8">
          <h2 className="text-sm font-mono font-normal tracking-mono-wide text-gray-900 dark:text-gray-100">Beads</h2>
          <span className="px-2.5 py-1 text-xs font-mono font-medium tabular-nums tracking-mono-normal rounded-full bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
            {filteredBeads.length}
          </span>
        </div>
        <div className="flex items-center gap-2">
          {/* Sort mode selector */}
          <button
            onClick={() => setSortMode(sortMode === 'priority' ? 'updated' : 'priority')}
            className={`flex items-center gap-1.5 px-2 py-1.5 rounded-lg text-xs font-medium transition-all ${
              sortMode === 'updated'
                ? 'bg-indigo-100 dark:bg-indigo-900/50 ring-1 ring-indigo-500 text-indigo-700 dark:text-indigo-300'
                : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400'
            }`}
            title={sortMode === 'priority' ? 'Sort by last updated' : 'Sort by priority'}
          >
            <ArrowUpDown className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">{sortMode === 'priority' ? 'Priority' : 'Updated'}</span>
          </button>
          {/* Show completed/archived toggle */}
          {completedBeadsCount > 0 && onToggleShowCompleted && (
            <button
              onClick={onToggleShowCompleted}
              className={`flex items-center gap-1.5 px-2 py-1.5 rounded-lg text-xs font-medium transition-all ${
                showCompleted
                  ? 'bg-green-100 dark:bg-green-900/50 ring-1 ring-green-500 text-green-700 dark:text-green-300'
                  : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400'
              }`}
              title={showCompleted ? 'Hide completed beads' : 'Show completed beads'}
            >
              <CheckCircle2 className="w-3.5 h-3.5" />
              <span className="tabular-nums">{completedBeadsCount}</span>
            </button>
          )}
          {/* View mode toggle - only show if there are dependencies */}
          {hasDependencies && (
            <button
              onClick={() => setViewMode(viewMode === 'hierarchy' ? 'flat' : 'hierarchy')}
              className={`p-2 rounded-lg transition-colors ${
                viewMode === 'hierarchy'
                  ? 'bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400'
                  : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400'
              }`}
              title={viewMode === 'hierarchy' ? 'Switch to flat view' : 'Switch to hierarchy view'}
            >
              <GitBranch className="w-4 h-4" />
            </button>
          )}
          <button
            onClick={onToggle}
            className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors group"
            title="Collapse beads pane"
          >
            <ChevronLeft className="w-4 h-4 text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-200" />
          </button>
        </div>
      </div>

      {/* Merge Queue Status - Compact strip at top (auto-hides when empty) */}
      <MergeQueueStatus
        completed={mergeQueueData.completed}
        resolvers={mergeQueueData.resolvers}
        pending={mergeQueueData.pending}
        active_workers={mergeQueueData.active_workers}
        {...(onTaskClick && { onTaskClick })}
      />

      {/* Status Summary */}
      <div className="flex items-center gap-5 px-5 py-3.5 border-b border-gray-100 dark:border-gray-700/50 bg-gray-50 dark:bg-gray-800/50 min-h-[44px]">
        {statusCounts.running > 0 && (
          <div className="flex items-center gap-2.5 text-xs">
            <Loader2 className="w-4 h-4 animate-spin text-blue-500" />
            <span className="font-mono tabular-nums tracking-mono-normal text-blue-600 dark:text-blue-400">{statusCounts.running}</span>
          </div>
        )}
        {statusCounts.blocked > 0 && (
          <div className="flex items-center gap-2.5 text-xs">
            <Clock className="w-4 h-4 text-yellow-500" />
            <span className="font-mono tabular-nums tracking-mono-normal text-yellow-600 dark:text-yellow-400">{statusCounts.blocked}</span>
          </div>
        )}
        {statusCounts.ready > 0 && (
          <div className="flex items-center gap-2.5 text-xs">
            <Circle className="w-4 h-4 text-gray-400" />
            <span className="font-mono tabular-nums tracking-mono-normal text-gray-600 dark:text-gray-400">{statusCounts.ready}</span>
          </div>
        )}
        {statusCounts.failed > 0 && (
          <div className="flex items-center gap-2.5 text-xs">
            <AlertCircle className="w-4 h-4 text-red-500" />
            <span className="font-mono tabular-nums tracking-mono-normal text-red-600 dark:text-red-400">{statusCounts.failed}</span>
          </div>
        )}
        {incompleteBeads.length === 0 && !showCompleted && (
          <span className="text-xs tracking-wider text-gray-500 dark:text-gray-400">All done!</span>
        )}
        {showCompleted && completedBeadsCount > 0 && (
          <div className="flex items-center gap-2.5 text-xs">
            <CheckCircle2 className="w-4 h-4 text-green-500" />
            <span className="font-mono tabular-nums tracking-mono-normal text-green-600 dark:text-green-400">{completedBeadsCount}</span>
          </div>
        )}
      </div>

      {/* Beads List */}
      <div ref={listContainerRef} className="flex-1 overflow-y-auto">
        {filteredBeads.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-center px-4">
            <div className="w-12 h-12 rounded-full bg-green-100 dark:bg-green-900/30 flex items-center justify-center mb-3">
              <svg className="w-6 h-6 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
              </svg>
            </div>
            <p className="text-sm font-medium text-gray-700 dark:text-gray-300">No pending beads</p>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">All tasks are complete</p>
          </div>
        ) : viewMode === 'flat' ? (
          /* Flat view - original layout */
          <div className="divide-y divide-gray-100 dark:divide-gray-700/50">
            {filteredBeads.map((task: TaskState) => {
              const statusConfig = getStatusConfig(task.status);
              const priorityStyle = getPriorityStyle(task.priority);
              const isHighlighted = highlightedTaskId === task.id;
              const isSelected = selectedBeadId === task.id;
              const isCompletedOrArchived = task.status.toLowerCase() === 'done' || task.status.toLowerCase() === 'completed' || task.archived;

              return (
                <div
                  key={task.id}
                  ref={(el) => {
                    if (el) {
                      taskRefs.current.set(task.id, el);
                    } else {
                      taskRefs.current.delete(task.id);
                    }
                  }}
                  onClick={(e) => handleBeadClick(task.id, e)}
                  className={`px-5 py-4 transition-all cursor-pointer ${
                    isSelected
                      ? 'bg-indigo-100 dark:bg-indigo-900/50 ring-2 ring-indigo-500 ring-inset'
                      : isHighlighted
                        ? 'bg-blue-100 dark:bg-blue-900/50 ring-2 ring-blue-500 ring-inset'
                        : isCompletedOrArchived
                          ? 'bg-gray-50 dark:bg-gray-800/50 opacity-75 hover:bg-gray-100 dark:hover:bg-gray-700/50'
                          : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }`}
                >
                  {/* Top row: Priority, Status, ID */}
                  <div className="flex items-center gap-3 mb-3 min-h-6">
                    {/* Priority badge */}
                    <span className={`px-2.5 py-1 rounded text-2xs font-mono font-semibold tracking-mono-normal ${priorityStyle.bg} ${priorityStyle.text}`}>
                      {priorityStyle.label}
                    </span>

                    {/* Status badge */}
                    <span className={`inline-flex items-center gap-2 px-2.5 py-1 rounded text-2xs font-medium tracking-wider ${statusConfig.bg} ${statusConfig.text}`}>
                      {statusConfig.icon}
                      {statusConfig.label}
                    </span>

                    {/* Archived badge */}
                    {task.archived && (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-2xs font-medium tracking-wider bg-purple-100 dark:bg-purple-900/50 text-purple-600 dark:text-purple-400">
                        <Archive className="w-3 h-3" />
                        Archived
                      </span>
                    )}

                    {/* ID */}
                    <code className="ml-auto text-2xs font-mono tracking-mono-normal text-gray-400 dark:text-gray-500">
                      {truncateId(task.id)}
                    </code>
                  </div>

                  {/* Title */}
                  <p className={`text-sm leading-relaxed tracking-wide line-clamp-2 ${isCompletedOrArchived ? 'text-gray-500 dark:text-gray-400' : 'text-gray-800 dark:text-gray-200'}`}>
                    {task.title}
                  </p>

                  {/* Dependencies (if any) */}
                  {task.dependencies && task.dependencies.length > 0 && (
                    <div className="mt-3 flex items-center gap-2.5 text-2xs text-gray-500 dark:text-gray-400">
                      <span className="font-medium tracking-wider">Blocked by:</span>
                      <span className="font-mono tracking-mono-normal truncate">
                        {task.dependencies.map(d => truncateId(d, 8)).join(', ')}
                      </span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        ) : (
          /* Hierarchy view - tree layout with indentation */
          <div className="py-1">
            {flattenedTree.map((node) => {
              const { task, children, depth } = node;
              const statusConfig = getStatusConfig(task.status);
              const priorityStyle = getPriorityStyle(task.priority);
              const hasChildren = children.length > 0;
              const isCollapsed = collapsedNodes.has(task.id);
              const isHighlighted = highlightedTaskId === task.id;
              const isSelected = selectedBeadId === task.id;
              const isCompletedOrArchived = task.status.toLowerCase() === 'done' || task.status.toLowerCase() === 'completed' || task.archived;

              return (
                <div
                  key={task.id}
                  ref={(el) => {
                    if (el) {
                      taskRefs.current.set(task.id, el);
                    } else {
                      taskRefs.current.delete(task.id);
                    }
                  }}
                  onClick={(e) => handleBeadClick(task.id, e)}
                  className={`transition-all cursor-pointer ${
                    isSelected
                      ? 'bg-indigo-100 dark:bg-indigo-900/50 ring-2 ring-indigo-500 ring-inset'
                      : isHighlighted
                        ? 'bg-blue-100 dark:bg-blue-900/50 ring-2 ring-blue-500 ring-inset'
                        : isCompletedOrArchived
                          ? 'bg-gray-50 dark:bg-gray-800/50 opacity-75 hover:bg-gray-100 dark:hover:bg-gray-700/50'
                          : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }`}
                  style={{ paddingLeft: `${depth * 16 + 12}px` }}
                >
                  <div className="py-3 pr-4">
                    {/* Tree structure indicator */}
                    <div className="flex items-start gap-1">
                      {/* Expand/collapse button or tree line */}
                      <div className="flex-shrink-0 w-4 h-4 flex items-center justify-center">
                        {hasChildren ? (
                          <button
                            onClick={() => toggleNodeCollapsed(task.id)}
                            className="p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
                          >
                            {isCollapsed ? (
                              <ChevronRight className="w-3 h-3 text-gray-400" />
                            ) : (
                              <ChevronDown className="w-3 h-3 text-gray-400" />
                            )}
                          </button>
                        ) : depth > 0 ? (
                          <div className="w-3 h-3 flex items-center justify-center">
                            <div className="w-1.5 h-1.5 rounded-full bg-gray-300 dark:bg-gray-600" />
                          </div>
                        ) : null}
                      </div>

                      {/* Task content */}
                      <div className="flex-1 min-w-0">
                        {/* Top row: Priority, Status, ID */}
                        <div className="flex items-center gap-2.5 mb-2 flex-wrap min-h-6">
                          {/* Priority badge */}
                          <span className={`px-2 py-0.5 rounded text-2xs font-mono font-semibold tracking-mono-normal ${priorityStyle.bg} ${priorityStyle.text}`}>
                            {priorityStyle.label}
                          </span>

                          {/* Status badge */}
                          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-2xs font-medium tracking-wider ${statusConfig.bg} ${statusConfig.text}`}>
                            {statusConfig.icon}
                            {statusConfig.label}
                          </span>

                          {/* Archived badge */}
                          {task.archived && (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-2xs font-medium tracking-wider bg-purple-100 dark:bg-purple-900/50 text-purple-600 dark:text-purple-400">
                              <Archive className="w-3 h-3" />
                              Archived
                            </span>
                          )}

                          {/* ID */}
                          <code className="ml-auto text-2xs font-mono tracking-mono-normal text-gray-400 dark:text-gray-500">
                            {truncateId(task.id)}
                          </code>
                        </div>

                        {/* Title */}
                        <p className={`text-sm leading-relaxed tracking-wide line-clamp-2 ${isCompletedOrArchived ? 'text-gray-500 dark:text-gray-400' : 'text-gray-800 dark:text-gray-200'}`}>
                          {task.title}
                        </p>

                        {/* Child count indicator when collapsed */}
                        {hasChildren && isCollapsed && (
                          <div className="mt-2 text-2xs tracking-wider text-gray-400 dark:text-gray-500">
                            {children.length} blocked task{children.length !== 1 ? 's' : ''} hidden
                          </div>
                        )}
                      </div>
                    </div>
                  </div>

                  {/* Subtle divider between items */}
                  <div
                    className="border-b border-gray-100 dark:border-gray-700/50"
                    style={{ marginLeft: depth > 0 ? '20px' : '0' }}
                  />
                </div>
              );
            })}
          </div>
        )}
      </div>
      </div>

      {/* Resize Handle */}
      {onResizeStart && (
        <div
          onMouseDown={onResizeStart}
          className={`
            w-1.5 bg-gray-200 dark:bg-gray-700 cursor-ew-resize flex items-center justify-center
            hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors group flex-shrink-0
            ${isResizing ? 'bg-blue-500 dark:bg-blue-600' : ''}
          `}
          title="Drag to resize"
        >
          <GripVertical
            className={`w-3 h-4 text-gray-400 group-hover:text-gray-600 dark:group-hover:text-gray-300 ${
              isResizing ? 'text-blue-200' : ''
            }`}
          />
        </div>
      )}
    </div>
  );
};
