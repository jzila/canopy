import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import type { MergeCompletedItem, MergeResolverItem, MergePendingItem, MergeWorkerItem } from '../../api/client';

interface MergeQueueStatusProps {
  completed: MergeCompletedItem[];
  resolvers: MergeResolverItem[];
  pending: MergePendingItem[];
  activeWorkers: MergeWorkerItem[];
  onTaskClick?: (taskId: string) => void;
}

interface TooltipState {
  visible: boolean;
  taskId: string;
  title: string;
  status: string;
  x: number;
  y: number;
}

// Extract short bead ID from task ID
const extractShortId = (taskId: string): string => {
  if (taskId.startsWith('canopy-')) return taskId.slice(7);
  if (taskId.startsWith('beads-')) return taskId.slice(6);
  return taskId.length > 8 ? taskId.slice(-4) : taskId;
};

// Individual circle component for a merge item
interface MergeCircleProps {
  taskId: string;
  status: 'completed' | 'active' | 'queued' | 'resolving' | 'failed';
  success?: boolean;
  position?: number;
  onMouseEnter: (e: React.MouseEvent) => void;
  onMouseLeave: () => void;
  onClick?: () => void;
  isRecent?: boolean;
}

const MergeCircle: React.FC<MergeCircleProps> = ({
  taskId,
  status,
  success,
  onMouseEnter,
  onMouseLeave,
  onClick,
  isRecent,
}) => {
  const getCircleClasses = () => {
    // Base classes - compact circles that cluster together
    // Using negative margins for overlap effect when clustered
    const base = 'w-3 h-3 rounded-full cursor-pointer flex-shrink-0 transition-all duration-500 ease-out';

    switch (status) {
      case 'completed':
        // Recently completed items animate to separate from the group
        const completedBase = success
          ? `${base} bg-green-500/50 hover:bg-green-500/70`
          : `${base} bg-red-500/50 hover:bg-red-500/70`;
        return isRecent ? `${completedBase} animate-slide-separate` : completedBase;
      case 'active':
        return `${base} bg-blue-500 animate-heartbeat hover:bg-blue-400`;
      case 'resolving':
        return `${base} bg-amber-500 animate-heartbeat hover:bg-amber-400`;
      case 'queued':
        // Hollow circles for queued items - tighter border
        return `${base} border-[1.5px] border-gray-400 dark:border-gray-500 bg-transparent hover:border-gray-500 dark:hover:border-gray-400`;
      case 'failed':
        return `${base} bg-red-500/50 hover:bg-red-500/70`;
      default:
        return base;
    }
  };

  return (
    <div
      className={getCircleClasses()}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onClick={onClick}
      title={extractShortId(taskId)}
    />
  );
};

export const MergeQueueStatus: React.FC<MergeQueueStatusProps> = ({
  completed,
  resolvers,
  pending,
  activeWorkers,
  onTaskClick,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [tooltip, setTooltip] = useState<TooltipState | null>(null);
  const hoverTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [recentlyCompleted, setRecentlyCompleted] = useState<Set<string>>(new Set());
  const prevCompletedRef = useRef<string[]>([]);

  // Track newly completed items for animation
  useEffect(() => {
    const currentIds = completed.map(c => c.taskId);
    const prevIds = prevCompletedRef.current;

    // Find newly completed (in current but not in previous)
    const newlyCompleted = currentIds.filter(id => !prevIds.includes(id));

    if (newlyCompleted.length > 0) {
      setRecentlyCompleted(prev => {
        const next = new Set(prev);
        newlyCompleted.forEach(id => next.add(id));
        return next;
      });

      // Clear the "recent" status after animation completes (600ms)
      const timer = setTimeout(() => {
        setRecentlyCompleted(prev => {
          const next = new Set(prev);
          newlyCompleted.forEach(id => next.delete(id));
          return next;
        });
      }, 600);

      return () => clearTimeout(timer);
    }

    prevCompletedRef.current = currentIds;
  }, [completed]);

  // Check if there's any merge activity
  const hasActivity = completed.length > 0 || resolvers.length > 0 || pending.length > 0 || activeWorkers.length > 0;

  // Count active items (merging + resolving)
  const mergingCount = activeWorkers.length;
  const resolvingCount = resolvers.filter(r => r.status === 'running').length;
  const queuedCount = pending.length;

  // Handle hover with debounce
  const handleMouseEnter = useCallback((e: React.MouseEvent, taskId: string, title: string, status: string) => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current);
    }

    const rect = (e.target as HTMLElement).getBoundingClientRect();
    const containerRect = containerRef.current?.getBoundingClientRect();

    hoverTimeoutRef.current = setTimeout(() => {
      setTooltip({
        visible: true,
        taskId,
        title,
        status,
        x: rect.left - (containerRect?.left || 0) + rect.width / 2,
        y: rect.top - (containerRect?.top || 0) - 8,
      });
    }, 150);
  }, []);

  const handleMouseLeave = useCallback(() => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current);
    }
    setTooltip(null);
  }, []);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (hoverTimeoutRef.current) {
        clearTimeout(hoverTimeoutRef.current);
      }
    };
  }, []);

  // Get recent completed (max 6 to keep it compact)
  const recentCompleted = useMemo(() => {
    return completed.slice(-6);
  }, [completed]);

  // Don't render anything if no activity - this is compact chrome that only appears when needed
  if (!hasActivity) {
    return null;
  }

  return (
    <div ref={containerRef} className="relative px-4 py-2 border-b border-gray-100 dark:border-gray-700/50 bg-gray-50/30 dark:bg-gray-800/20">
      <div className="flex items-center gap-3">
        {/* Completed circles - faded, on the left, separated from active */}
        {recentCompleted.length > 0 && (
          <div className="flex items-center -space-x-0.5 opacity-50">
            {/* Show ellipsis if there are more completed items */}
            {completed.length > 6 && (
              <span className="text-2xs text-gray-400 dark:text-gray-500 mr-1.5 tabular-nums">
                +{completed.length - 6}
              </span>
            )}
            {recentCompleted.map((item) => (
              <MergeCircle
                key={`completed-${item.taskId}`}
                taskId={item.taskId}
                status={item.success ? 'completed' : 'failed'}
                success={item.success}
                isRecent={recentlyCompleted.has(item.taskId)}
                onMouseEnter={(e) => handleMouseEnter(e, item.taskId, extractShortId(item.taskId), item.success ? 'Merged' : 'Failed')}
                onMouseLeave={handleMouseLeave}
                onClick={() => onTaskClick?.(item.taskId)}
              />
            ))}
          </div>
        )}

        {/* Separator - thin arrow showing flow direction */}
        {recentCompleted.length > 0 && (mergingCount > 0 || resolvingCount > 0 || queuedCount > 0) && (
          <span className="text-gray-300 dark:text-gray-600 text-2xs opacity-60">←</span>
        )}

        {/* Active workers - bright with gentle heartbeat */}
        {activeWorkers.length > 0 && (
          <div className="flex items-center -space-x-0.5">
            {activeWorkers.map((worker) => (
              <MergeCircle
                key={`active-${worker.taskId}`}
                taskId={worker.taskId}
                status="active"
                onMouseEnter={(e) => handleMouseEnter(e, worker.taskId, extractShortId(worker.taskId), 'Merging')}
                onMouseLeave={handleMouseLeave}
                onClick={() => onTaskClick?.(worker.taskId)}
              />
            ))}
          </div>
        )}

        {/* Resolvers - amber with gentle heartbeat */}
        {resolvers.filter(r => r.status === 'running').length > 0 && (
          <div className="flex items-center -space-x-0.5">
            {resolvers.filter(r => r.status === 'running').map((resolver) => (
              <MergeCircle
                key={`resolver-${resolver.resolverTaskId}`}
                taskId={resolver.resolverTaskId}
                status="resolving"
                onMouseEnter={(e) => handleMouseEnter(e, resolver.resolverTaskId, extractShortId(resolver.resolverTaskId), 'Resolving')}
                onMouseLeave={handleMouseLeave}
                onClick={() => onTaskClick?.(resolver.resolverTaskId)}
              />
            ))}
          </div>
        )}

        {/* Separator - thin arrow */}
        {(mergingCount > 0 || resolvingCount > 0) && queuedCount > 0 && (
          <span className="text-gray-300 dark:text-gray-600 text-2xs opacity-60">←</span>
        )}

        {/* Pending - hollow circles tightly clustered */}
        {pending.length > 0 && (
          <div className="flex items-center -space-x-1">
            {pending.slice(0, 5).map((item) => (
              <MergeCircle
                key={`pending-${item.taskId}`}
                taskId={item.taskId}
                status="queued"
                position={item.position}
                onMouseEnter={(e) => handleMouseEnter(e, item.taskId, extractShortId(item.taskId), `#${item.position}`)}
                onMouseLeave={handleMouseLeave}
                onClick={() => onTaskClick?.(item.taskId)}
              />
            ))}
            {pending.length > 5 && (
              <span className="text-2xs text-gray-400 dark:text-gray-500 ml-1.5 tabular-nums">
                +{pending.length - 5}
              </span>
            )}
          </div>
        )}

        {/* Compact status summary - right aligned */}
        <div className="ml-auto flex items-center gap-2 text-2xs text-gray-500 dark:text-gray-400 tabular-nums">
          {mergingCount > 0 && (
            <span className="flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-blue-500 animate-heartbeat" />
              <span>{mergingCount}</span>
            </span>
          )}
          {resolvingCount > 0 && (
            <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-heartbeat" />
              <span>{resolvingCount}</span>
            </span>
          )}
          {queuedCount > 0 && (
            <span className="text-gray-400 dark:text-gray-500">
              {queuedCount}q
            </span>
          )}
        </div>
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="absolute z-50 pointer-events-none transform -translate-x-1/2 -translate-y-full"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          <div className="bg-gray-900 text-white text-2xs px-2.5 py-1.5 rounded shadow-lg whitespace-nowrap">
            <span className="font-mono">{tooltip.title}</span>
            <span className="text-gray-400 ml-2">{tooltip.status}</span>
          </div>
        </div>
      )}
    </div>
  );
};

export default MergeQueueStatus;
