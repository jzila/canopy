import React, { useMemo, useRef, useEffect, useCallback, useState } from 'react';
import {
  Clock,
  DollarSign,
  FileEdit,
  GitCommit,
  AlertTriangle,
  Zap,
  X,
  ExternalLink,
  Terminal,
} from 'lucide-react';
import type {
  MergeQueueTreeProps,
  TreeNode,
  TreeConnection,
  TreeLayout,
  ResolverBranch,
  NodeDetails,
} from './types';

// Default layout configuration
const DEFAULT_LAYOUT: TreeLayout = {
  nodeRadius: 12,
  nodeSpacing: 80,
  trunkY: 60,
  branchOffset: 50,
  padding: 40,
};

// Track newly created nodes/connections for animation
interface AnimationState {
  newNodeIds: Set<string>;
  newConnectionIds: Set<string>;
}

// Tooltip state
interface TooltipState {
  visible: boolean;
  nodeId: string | null;
  x: number;
  y: number;
}

// Format duration in a human-readable way
const formatDuration = (seconds: number): string => {
  if (seconds < 60) {
    return `${Math.round(seconds)}s`;
  }
  const minutes = Math.floor(seconds / 60);
  const secs = Math.round(seconds % 60);
  if (minutes < 60) {
    return secs > 0 ? `${minutes}m ${secs}s` : `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
};

// Format tokens with K/M suffix
const formatTokens = (tokens: number): string => {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(1)}M`;
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`;
  }
  return tokens.toString();
};

// Format cost
const formatCost = (cost: number): string => {
  if (cost < 0.01) {
    return `$${(cost * 100).toFixed(2)}c`;
  }
  return `$${cost.toFixed(2)}`;
};

// Enhanced Tooltip Component
interface EnhancedTooltipProps {
  node: TreeNode;
  details: NodeDetails | null;
  x: number;
  y: number;
  containerRef: React.RefObject<HTMLDivElement>;
}

const EnhancedTooltip: React.FC<EnhancedTooltipProps> = ({
  node,
  details,
  x,
  y,
  containerRef,
}) => {
  const tooltipRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: x, top: y - 10 });

  // Adjust tooltip position to stay within viewport
  useEffect(() => {
    if (tooltipRef.current && containerRef.current) {
      const tooltip = tooltipRef.current;
      const container = containerRef.current;
      const containerRect = container.getBoundingClientRect();
      const tooltipRect = tooltip.getBoundingClientRect();

      let left = x - tooltipRect.width / 2;
      let top = y - tooltipRect.height - 15;

      // Keep within container bounds
      const scrollLeft = container.scrollLeft;
      if (left < scrollLeft + 10) {
        left = scrollLeft + 10;
      } else if (left + tooltipRect.width > scrollLeft + containerRect.width - 10) {
        left = scrollLeft + containerRect.width - tooltipRect.width - 10;
      }

      // If tooltip goes above container, show below node
      if (top < 5) {
        top = y + 30;
      }

      setPosition({ left, top });
    }
  }, [x, y, containerRef]);

  const statusLabel = node.status === 'success' ? 'Completed' :
    node.status === 'failed' ? 'Failed' :
    node.status === 'resolving' ? 'Resolving' :
    node.status === 'resolved' ? 'Resolved' :
    node.status === 'active' ? 'Active' : 'Pending';

  const statusColor = node.status === 'success' || node.status === 'resolved' ? 'text-green-400' :
    node.status === 'failed' ? 'text-red-400' :
    node.status === 'resolving' || node.status === 'active' ? 'text-blue-400' : 'text-gray-400';

  return (
    <div
      ref={tooltipRef}
      className="absolute z-50 pointer-events-none"
      style={{ left: position.left, top: position.top }}
    >
      <div className="bg-gray-900 border border-gray-700 rounded-lg shadow-xl p-3 min-w-[220px] max-w-[300px]">
        {/* Header */}
        <div className="border-b border-gray-700 pb-2 mb-2">
          <div className="font-mono text-sm text-gray-100 font-medium truncate">
            {node.taskId}
          </div>
          {details?.title && (
            <div className="text-xs text-gray-400 mt-0.5 line-clamp-2">
              {details.title}
            </div>
          )}
          <div className={`text-xs font-medium mt-1 ${statusColor}`}>
            {statusLabel}
          </div>
        </div>

        {/* Details */}
        <div className="space-y-1.5 text-xs">
          {/* Duration */}
          {details?.duration !== undefined && details.duration > 0 && (
            <div className="flex items-center gap-2 text-gray-300">
              <Clock className="w-3.5 h-3.5 text-gray-500" />
              <span>Duration: {formatDuration(details.duration)}</span>
            </div>
          )}

          {/* Token usage */}
          {details?.tokenUsage && details.tokenUsage.total_tokens > 0 && (
            <div className="flex items-center gap-2 text-gray-300">
              <Zap className="w-3.5 h-3.5 text-gray-500" />
              <span>Tokens: {formatTokens(details.tokenUsage.total_tokens)}</span>
              {details.tokenUsage.cost_usd > 0 && (
                <span className="text-gray-500">({formatCost(details.tokenUsage.cost_usd)})</span>
              )}
            </div>
          )}

          {/* Files changed */}
          {details?.filesChanged !== undefined && details.filesChanged > 0 && (
            <div className="flex items-center gap-2 text-gray-300">
              <FileEdit className="w-3.5 h-3.5 text-gray-500" />
              <span>Files changed: {details.filesChanged}</span>
            </div>
          )}

          {/* Commits */}
          {details?.commitCount !== undefined && details.commitCount > 0 && (
            <div className="flex items-center gap-2 text-gray-300">
              <GitCommit className="w-3.5 h-3.5 text-gray-500" />
              <span>Commits: {details.commitCount}</span>
            </div>
          )}

          {/* Error message */}
          {node.error && (
            <div className="flex items-start gap-2 text-red-400 mt-2 pt-2 border-t border-gray-700">
              <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
              <span className="line-clamp-3">{node.error}</span>
            </div>
          )}
        </div>

        {/* Click hint */}
        <div className="mt-2 pt-2 border-t border-gray-700 text-xs text-gray-500 text-center">
          Click for details
        </div>
      </div>
    </div>
  );
};

// Detail Panel Component (shown on click)
interface DetailPanelProps {
  node: TreeNode;
  details: NodeDetails | null;
  onClose: () => void;
  onViewAgent: () => void;
}

const DetailPanel: React.FC<DetailPanelProps> = ({
  node,
  details,
  onClose,
  onViewAgent,
}) => {
  const statusLabel = node.status === 'success' ? 'Completed' :
    node.status === 'failed' ? 'Failed' :
    node.status === 'resolving' ? 'Resolving' :
    node.status === 'resolved' ? 'Resolved' :
    node.status === 'active' ? 'Active' : 'Pending';

  const statusBadgeClass = node.status === 'success' || node.status === 'resolved'
    ? 'bg-green-500/20 text-green-400 border-green-500/30'
    : node.status === 'failed'
    ? 'bg-red-500/20 text-red-400 border-red-500/30'
    : node.status === 'resolving' || node.status === 'active'
    ? 'bg-blue-500/20 text-blue-400 border-blue-500/30'
    : 'bg-gray-500/20 text-gray-400 border-gray-500/30';

  return (
    <div className="absolute inset-0 z-50 bg-gray-900/90 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="bg-gray-800 border border-gray-700 rounded-lg shadow-2xl max-w-lg w-full max-h-[80%] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <div className="flex-1 min-w-0">
            <h3 className="font-mono text-lg text-gray-100 font-medium truncate">
              {node.taskId}
            </h3>
            {details?.title && (
              <p className="text-sm text-gray-400 mt-0.5 truncate">
                {details.title}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2 ml-4">
            <span className={`px-2 py-1 text-xs font-medium rounded border ${statusBadgeClass}`}>
              {statusLabel}
            </span>
            <button
              onClick={onClose}
              className="p-1.5 rounded hover:bg-gray-700 transition-colors text-gray-400 hover:text-gray-200"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Stats Grid */}
          <div className="grid grid-cols-2 gap-3">
            {details?.duration !== undefined && details.duration > 0 && (
              <div className="bg-gray-900/50 rounded-lg p-3">
                <div className="flex items-center gap-2 text-gray-400 text-xs mb-1">
                  <Clock className="w-3.5 h-3.5" />
                  Duration
                </div>
                <div className="text-lg font-medium text-gray-100">
                  {formatDuration(details.duration)}
                </div>
              </div>
            )}

            {details?.tokenUsage && details.tokenUsage.total_tokens > 0 && (
              <div className="bg-gray-900/50 rounded-lg p-3">
                <div className="flex items-center gap-2 text-gray-400 text-xs mb-1">
                  <Zap className="w-3.5 h-3.5" />
                  Tokens
                </div>
                <div className="text-lg font-medium text-gray-100">
                  {formatTokens(details.tokenUsage.total_tokens)}
                </div>
                {details.tokenUsage.cost_usd > 0 && (
                  <div className="text-xs text-gray-500">
                    {formatCost(details.tokenUsage.cost_usd)}
                  </div>
                )}
              </div>
            )}

            {details?.filesChanged !== undefined && (
              <div className="bg-gray-900/50 rounded-lg p-3">
                <div className="flex items-center gap-2 text-gray-400 text-xs mb-1">
                  <FileEdit className="w-3.5 h-3.5" />
                  Files Changed
                </div>
                <div className="text-lg font-medium text-gray-100">
                  {details.filesChanged}
                </div>
              </div>
            )}

            {details?.commitCount !== undefined && (
              <div className="bg-gray-900/50 rounded-lg p-3">
                <div className="flex items-center gap-2 text-gray-400 text-xs mb-1">
                  <GitCommit className="w-3.5 h-3.5" />
                  Commits
                </div>
                <div className="text-lg font-medium text-gray-100">
                  {details.commitCount}
                </div>
              </div>
            )}
          </div>

          {/* Git Commits List */}
          {details?.commits && details.commits.length > 0 && (
            <div>
              <h4 className="text-sm font-medium text-gray-300 mb-2">Git Commits</h4>
              <div className="space-y-2">
                {details.commits.map((commit, idx) => (
                  <div key={commit.hash || idx} className="bg-gray-900/50 rounded-lg p-3">
                    <div className="flex items-center gap-2 mb-1">
                      <code className="text-xs text-blue-400 font-mono">
                        {commit.short_hash || commit.hash?.slice(0, 7)}
                      </code>
                      <span className="text-xs text-gray-500">
                        {commit.author}
                      </span>
                    </div>
                    <p className="text-sm text-gray-300 line-clamp-2">
                      {commit.message}
                    </p>
                    {commit.files_changed && commit.files_changed.length > 0 && (
                      <div className="mt-1 text-xs text-gray-500">
                        {commit.files_changed.length} file{commit.files_changed.length !== 1 ? 's' : ''} changed
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Error Message */}
          {node.error && (
            <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-3">
              <div className="flex items-center gap-2 text-red-400 text-sm font-medium mb-2">
                <AlertTriangle className="w-4 h-4" />
                Error
              </div>
              <pre className="text-sm text-red-300 whitespace-pre-wrap font-mono">
                {node.error}
              </pre>
            </div>
          )}
        </div>

        {/* Footer Actions */}
        <div className="p-4 border-t border-gray-700 flex gap-3">
          <button
            onClick={onViewAgent}
            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors text-sm font-medium"
          >
            <Terminal className="w-4 h-4" />
            View Agent Output
          </button>
          <button
            onClick={() => window.open(`#task-${node.taskId}`, '_blank')}
            className="flex items-center justify-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 rounded-lg transition-colors text-sm"
            title="Open in beads"
          >
            <ExternalLink className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  );
};

// Extract short bead ID from task ID (e.g., 'abc1' from 'canopy-abc1')
const extractShortId = (taskId: string): string => {
  // Handle 'canopy-xxx' format
  if (taskId.startsWith('canopy-')) {
    return taskId.slice(7); // Remove 'canopy-' prefix
  }
  // Handle 'beads-xxx' format
  if (taskId.startsWith('beads-')) {
    return taskId.slice(6); // Remove 'beads-' prefix
  }
  // Fallback: return last 4 characters if ID is long
  return taskId.length > 8 ? taskId.slice(-4) : taskId;
};

// Node component for rendering individual nodes
interface NodeProps {
  node: TreeNode;
  layout: TreeLayout;
  isHovered: boolean;
  isNew?: boolean;
  onClick?: () => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
}

const TreeNodeComponent: React.FC<NodeProps> = ({
  node,
  layout,
  isHovered,
  isNew = false,
  onClick,
  onMouseEnter,
  onMouseLeave,
}) => {
  const { nodeRadius } = layout;
  const { x, y, type, status } = node;
  const [animationComplete, setAnimationComplete] = useState(!isNew);

  // Handle entry animation completion
  useEffect(() => {
    if (isNew) {
      const timer = setTimeout(() => setAnimationComplete(true), 400);
      return () => clearTimeout(timer);
    }
  }, [isNew]);

  // Determine fill and stroke based on node type and status
  const getNodeStyle = () => {
    switch (type) {
      case 'completed':
        return {
          fill: status === 'success' ? '#22c55e' : '#ef4444', // green-500 / red-500
          stroke: status === 'success' ? '#16a34a' : '#dc2626', // green-600 / red-600
          strokeWidth: 2,
          fillOpacity: 1,
        };
      case 'pending':
        return {
          fill: 'transparent',
          stroke: '#6b7280', // gray-500
          strokeWidth: 2,
          strokeDasharray: '4,2',
          fillOpacity: 0,
        };
      case 'active':
        return {
          fill: '#3b82f6', // blue-500
          stroke: '#2563eb', // blue-600
          strokeWidth: 2,
          fillOpacity: 1,
        };
      case 'resolver':
        return {
          fill: status === 'resolved' ? '#22c55e' : status === 'failed' ? '#ef4444' : '#f59e0b', // amber-500
          stroke: status === 'resolved' ? '#16a34a' : status === 'failed' ? '#dc2626' : '#d97706', // amber-600
          strokeWidth: 2,
          fillOpacity: 1,
        };
      default:
        return {
          fill: '#6b7280',
          stroke: '#4b5563',
          strokeWidth: 2,
          fillOpacity: 1,
        };
    }
  };

  const style = getNodeStyle();
  const isAnimated = type === 'active' || (type === 'resolver' && status === 'resolving');

  // Animation styles for new nodes
  const entryStyle = isNew && !animationComplete
    ? {
        opacity: 0,
        transform: type === 'resolver' ? `translate(0, -20px) scale(0.5)` : 'scale(0.5)',
        animation: 'nodeEntry 0.4s ease-out forwards',
      }
    : {};

  return (
    <g
      className={`tree-node cursor-pointer transition-transform ${isHovered ? 'scale-110' : ''}`}
      style={{ transformOrigin: `${x}px ${y}px`, ...entryStyle }}
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      {/* Pulse animation ring for active nodes */}
      {isAnimated && (
        <circle
          cx={x}
          cy={y}
          r={nodeRadius + 4}
          fill="none"
          stroke={style.stroke}
          strokeWidth={2}
          opacity={0.5}
          className="animate-ping"
        />
      )}

      {/* Main node circle */}
      <circle
        cx={x}
        cy={y}
        r={nodeRadius}
        fill={style.fill}
        stroke={style.stroke}
        strokeWidth={style.strokeWidth}
        strokeDasharray={style.strokeDasharray}
        fillOpacity={style.fillOpacity}
        className={isAnimated ? 'animate-pulse' : ''}
      />

      {/* Status indicator icon */}
      {type === 'completed' && (
        <text
          x={x}
          y={y + 4}
          textAnchor="middle"
          fontSize="14"
          fill="white"
          fontWeight="bold"
        >
          {status === 'success' ? '\u2713' : '\u2717'}
        </text>
      )}

      {/* Label below node - shows short bead ID */}
      <text
        x={x}
        y={y + nodeRadius + 16}
        textAnchor="middle"
        fontSize="10"
        fill="#9ca3af"
        className="font-mono"
      >
        {extractShortId(node.taskId)}
      </text>
    </g>
  );
};

// Connection line component
interface ConnectionProps {
  connection: TreeConnection;
  isNew?: boolean;
}

const ConnectionLine: React.FC<ConnectionProps> = ({ connection, isNew = false }) => {
  const { fromX, fromY, toX, toY, type } = connection;
  const pathRef = useRef<SVGPathElement>(null);

  // Calculate control points for curved paths
  const getMidX = () => (fromX + toX) / 2;

  const getPathStyle = () => {
    switch (type) {
      case 'trunk':
        return {
          stroke: '#4b5563', // gray-600
          strokeWidth: 3,
          strokeDasharray: undefined,
        };
      case 'branch':
        return {
          stroke: '#f59e0b', // amber-500
          strokeWidth: 2,
          strokeDasharray: undefined,
        };
      case 'merge':
        return {
          stroke: '#22c55e', // green-500
          strokeWidth: 2,
          strokeDasharray: '4,2',
        };
      default:
        return {
          stroke: '#6b7280',
          strokeWidth: 2,
          strokeDasharray: undefined,
        };
    }
  };

  const style = getPathStyle();

  // Generate SVG path based on connection type
  const getPath = () => {
    if (type === 'trunk') {
      // Straight horizontal line for trunk
      return `M ${fromX} ${fromY} L ${toX} ${toY}`;
    } else if (type === 'branch') {
      // Curved path going down for branch
      const midX = getMidX();
      return `M ${fromX} ${fromY} C ${midX} ${fromY}, ${midX} ${toY}, ${toX} ${toY}`;
    } else {
      // Curved path going up for merge
      const midX = getMidX();
      return `M ${fromX} ${fromY} C ${midX} ${fromY}, ${midX} ${toY}, ${toX} ${toY}`;
    }
  };

  // Animate path drawing for new connections
  useEffect(() => {
    if (isNew && pathRef.current) {
      const path = pathRef.current;
      const length = path.getTotalLength();

      // Set up initial state for animation
      path.style.strokeDasharray = `${length}`;
      path.style.strokeDashoffset = `${length}`;

      // Trigger reflow to ensure initial state is applied
      path.getBoundingClientRect();

      // Animate to full path
      path.style.transition = 'stroke-dashoffset 0.5s ease-out';
      path.style.strokeDashoffset = '0';

      // Clean up animation styles after completion
      const cleanup = setTimeout(() => {
        path.style.transition = '';
        path.style.strokeDasharray = style.strokeDasharray || '';
        path.style.strokeDashoffset = '';
      }, 600);

      return () => clearTimeout(cleanup);
    }
  }, [isNew, style.strokeDasharray]);

  return (
    <path
      ref={pathRef}
      d={getPath()}
      fill="none"
      stroke={style.stroke}
      strokeWidth={style.strokeWidth}
      strokeDasharray={isNew ? undefined : style.strokeDasharray}
      strokeLinecap="round"
      className="transition-all duration-300"
    />
  );
};

// Main MergeQueueTree component
export const MergeQueueTree: React.FC<MergeQueueTreeProps> = ({
  completed,
  resolvers,
  pending,
  activeWorkers,
  onNodeClick,
  onNodeHover,
  getNodeDetails,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [tooltipState, setTooltipState] = useState<TooltipState>({
    visible: false,
    nodeId: null,
    x: 0,
    y: 0,
  });
  const hoverTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const layout = DEFAULT_LAYOUT;

  // Track previously seen node/connection IDs for animation
  const seenNodeIdsRef = useRef<Set<string>>(new Set());
  const seenConnectionIdsRef = useRef<Set<string>>(new Set());
  const [animationState, setAnimationState] = useState<AnimationState>({
    newNodeIds: new Set(),
    newConnectionIds: new Set(),
  });

  // Debounced hover handler
  const handleDebouncedHover = useCallback(
    (nodeId: string | null, x: number, y: number) => {
      // Clear any pending hover timeout
      if (hoverTimeoutRef.current) {
        clearTimeout(hoverTimeoutRef.current);
        hoverTimeoutRef.current = null;
      }

      if (nodeId) {
        // Debounce showing tooltip (150ms)
        hoverTimeoutRef.current = setTimeout(() => {
          setHoveredNodeId(nodeId);
          setTooltipState({ visible: true, nodeId, x, y });
          onNodeHover?.(nodeId);
        }, 150);
      } else {
        // Hide tooltip immediately on leave
        setHoveredNodeId(null);
        setTooltipState({ visible: false, nodeId: null, x: 0, y: 0 });
        onNodeHover?.(null);
      }
    },
    [onNodeHover]
  );

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (hoverTimeoutRef.current) {
        clearTimeout(hoverTimeoutRef.current);
      }
    };
  }, []);

  // Build nodes and connections from props
  const { nodes, connections, svgWidth, svgHeight } = useMemo(() => {
    const nodeList: TreeNode[] = [];
    const connectionList: TreeConnection[] = [];
    let currentX = layout.padding;
    const trunkY = layout.trunkY;

    // Track resolver relationships for connecting lines
    const resolverMap = new Map<string, ResolverBranch[]>();
    resolvers.forEach((resolver) => {
      const existing = resolverMap.get(resolver.parentTaskId) || [];
      existing.push(resolver);
      resolverMap.set(resolver.parentTaskId, existing);
    });

    // Add completed nodes to trunk
    completed.forEach((item, index) => {
      const x = currentX + index * layout.nodeSpacing;
      const nodeId = `completed-${item.taskId}`;

      const completedNode: TreeNode = {
        id: nodeId,
        taskId: item.taskId,
        agentId: item.agentId,
        type: 'completed',
        status: item.success ? 'success' : 'failed',
        x,
        y: trunkY,
      };
      if (item.error) {
        completedNode.error = item.error;
      }
      nodeList.push(completedNode);

      // Add trunk connection to previous node
      if (index > 0) {
        const prevItem = completed[index - 1];
        if (!prevItem) return;
        const prevNodeId = `completed-${prevItem.taskId}`;
        connectionList.push({
          id: `trunk-${index}`,
          fromNode: prevNodeId,
          toNode: nodeId,
          type: 'trunk',
          fromX: x - layout.nodeSpacing + layout.nodeRadius,
          fromY: trunkY,
          toX: x - layout.nodeRadius,
          toY: trunkY,
        });
      }

      // Check for resolvers branching from this completed node
      const itemResolvers = resolverMap.get(item.taskId);
      if (itemResolvers) {
        itemResolvers.forEach((resolver, resolverIndex) => {
          const resolverY = trunkY + layout.branchOffset + resolverIndex * 40;
          const resolverX = x + layout.nodeSpacing / 3;
          const resolverNodeId = `resolver-${resolver.resolverTaskId}`;

          // Add resolver node
          nodeList.push({
            id: resolverNodeId,
            taskId: resolver.resolverTaskId,
            agentId: resolver.resolverAgentId,
            type: 'resolver',
            status: resolver.status,
            x: resolverX,
            y: resolverY,
            parentId: nodeId,
          });

          // Branch connection from parent to resolver
          connectionList.push({
            id: `branch-${resolver.resolverTaskId}`,
            fromNode: nodeId,
            toNode: resolverNodeId,
            type: 'branch',
            fromX: x,
            fromY: trunkY + layout.nodeRadius,
            toX: resolverX,
            toY: resolverY - layout.nodeRadius,
          });

          // If resolver is resolved, add merge line back to trunk
          if (resolver.status === 'resolved' && index < completed.length - 1) {
            const nextItem = completed[index + 1];
            if (!nextItem) return;
            const nextNodeX = x + layout.nodeSpacing;
            connectionList.push({
              id: `merge-${resolver.resolverTaskId}`,
              fromNode: resolverNodeId,
              toNode: `completed-${nextItem.taskId}`,
              type: 'merge',
              fromX: resolverX + layout.nodeRadius,
              fromY: resolverY,
              toX: nextNodeX - layout.nodeRadius,
              toY: trunkY,
            });
          }
        });
      }
    });

    // Calculate starting X for active workers and pending items
    currentX = layout.padding + completed.length * layout.nodeSpacing;

    // Add active workers on the trunk
    activeWorkers.forEach((worker, index) => {
      const x = currentX + index * layout.nodeSpacing;
      const nodeId = `active-${worker.taskId}`;

      nodeList.push({
        id: nodeId,
        taskId: worker.taskId,
        agentId: worker.agentId,
        type: 'active',
        status: 'active',
        x,
        y: trunkY,
      });

      // Connect to previous node
      const prevNodeIndex = completed.length + index - 1;
      if (prevNodeIndex >= 0) {
        let prevNodeId: string | null = null;
        if (index === 0) {
          const lastCompleted = completed[completed.length - 1];
          if (lastCompleted) {
            prevNodeId = `completed-${lastCompleted.taskId}`;
          }
        } else {
          const prevWorker = activeWorkers[index - 1];
          if (prevWorker) {
            prevNodeId = `active-${prevWorker.taskId}`;
          }
        }

        if (prevNodeId) {
          connectionList.push({
            id: `trunk-active-${index}`,
            fromNode: prevNodeId,
            toNode: nodeId,
            type: 'trunk',
            fromX: x - layout.nodeSpacing + layout.nodeRadius,
            fromY: trunkY,
            toX: x - layout.nodeRadius,
            toY: trunkY,
          });
        }
      }

      // Check for resolvers on active workers
      const workerResolvers = resolverMap.get(worker.taskId);
      if (workerResolvers) {
        workerResolvers.forEach((resolver, resolverIndex) => {
          const resolverY = trunkY + layout.branchOffset + resolverIndex * 40;
          const resolverX = x + layout.nodeSpacing / 3;
          const resolverNodeId = `resolver-${resolver.resolverTaskId}`;

          nodeList.push({
            id: resolverNodeId,
            taskId: resolver.resolverTaskId,
            agentId: resolver.resolverAgentId,
            type: 'resolver',
            status: resolver.status,
            x: resolverX,
            y: resolverY,
            parentId: nodeId,
          });

          connectionList.push({
            id: `branch-${resolver.resolverTaskId}`,
            fromNode: nodeId,
            toNode: resolverNodeId,
            type: 'branch',
            fromX: x,
            fromY: trunkY + layout.nodeRadius,
            toX: resolverX,
            toY: resolverY - layout.nodeRadius,
          });
        });
      }
    });

    // Update current X for pending items
    currentX = layout.padding + (completed.length + activeWorkers.length) * layout.nodeSpacing;

    // Add pending nodes
    pending.forEach((item, index) => {
      const x = currentX + index * layout.nodeSpacing;
      const nodeId = `pending-${item.taskId}`;

      nodeList.push({
        id: nodeId,
        taskId: item.taskId,
        agentId: item.agentId,
        type: 'pending',
        status: 'pending',
        x,
        y: trunkY,
        label: `#${item.position}`,
      });

      // Connect to previous node
      const prevNodeIndex = completed.length + activeWorkers.length + index - 1;
      if (prevNodeIndex >= 0) {
        let prevNodeId: string | null = null;

        if (index === 0) {
          const lastWorker = activeWorkers[activeWorkers.length - 1];
          const lastCompleted = completed[completed.length - 1];
          if (lastWorker) {
            prevNodeId = `active-${lastWorker.taskId}`;
          } else if (lastCompleted) {
            prevNodeId = `completed-${lastCompleted.taskId}`;
          }
        } else {
          const prevPending = pending[index - 1];
          if (prevPending) {
            prevNodeId = `pending-${prevPending.taskId}`;
          }
        }

        if (prevNodeId) {
          connectionList.push({
            id: `trunk-pending-${index}`,
            fromNode: prevNodeId,
            toNode: nodeId,
            type: 'trunk',
            fromX: x - layout.nodeSpacing + layout.nodeRadius,
            fromY: trunkY,
            toX: x - layout.nodeRadius,
            toY: trunkY,
          });
        }
      }
    });

    // Calculate SVG dimensions
    const totalNodes = completed.length + activeWorkers.length + pending.length;
    const calculatedWidth = Math.max(
      layout.padding * 2 + totalNodes * layout.nodeSpacing,
      300
    );

    // Find max Y for height calculation (account for resolver branches)
    const maxY = Math.max(
      trunkY + layout.nodeRadius + 30,
      ...nodeList.map((n) => n.y + layout.nodeRadius + 30)
    );

    return {
      nodes: nodeList,
      connections: connectionList,
      svgWidth: calculatedWidth,
      svgHeight: maxY + layout.padding,
    };
  }, [completed, resolvers, pending, activeWorkers, layout]);

  // Detect new nodes and connections for animation
  useEffect(() => {
    const newNodeIds = new Set<string>();
    const newConnectionIds = new Set<string>();

    // Find new nodes
    nodes.forEach((node) => {
      if (!seenNodeIdsRef.current.has(node.id)) {
        newNodeIds.add(node.id);
        seenNodeIdsRef.current.add(node.id);
      }
    });

    // Find new connections
    connections.forEach((conn) => {
      if (!seenConnectionIdsRef.current.has(conn.id)) {
        newConnectionIds.add(conn.id);
        seenConnectionIdsRef.current.add(conn.id);
      }
    });

    // Update animation state if there are new items
    if (newNodeIds.size > 0 || newConnectionIds.size > 0) {
      setAnimationState({ newNodeIds, newConnectionIds });

      // Clear animation state after animation completes
      const timer = setTimeout(() => {
        setAnimationState({ newNodeIds: new Set(), newConnectionIds: new Set() });
      }, 700);

      return () => clearTimeout(timer);
    }
  }, [nodes, connections]);

  // Auto-scroll to the right (newest nodes)
  useEffect(() => {
    if (containerRef.current) {
      const container = containerRef.current;
      // Smooth scroll to the right edge
      container.scrollTo({
        left: container.scrollWidth - container.clientWidth,
        behavior: 'smooth',
      });
    }
  }, [nodes.length, svgWidth]);

  // Handle node click - show detail panel
  const handleNodeClick = useCallback(
    (taskId: string, node: TreeNode) => {
      setSelectedNodeId(taskId);
      // Also call the external onNodeClick to select the agent in the main view
      onNodeClick?.(taskId);
    },
    [onNodeClick]
  );

  // Handle closing the detail panel
  const handleCloseDetailPanel = useCallback(() => {
    setSelectedNodeId(null);
  }, []);

  // Handle "View Agent Output" button - close panel and trigger selection
  const handleViewAgent = useCallback(() => {
    if (selectedNodeId) {
      onNodeClick?.(selectedNodeId);
    }
    setSelectedNodeId(null);
  }, [selectedNodeId, onNodeClick]);

  // Find the selected node from the nodes array
  const selectedNode = useMemo(() => {
    if (!selectedNodeId) return null;
    return nodes.find((n) => n.taskId === selectedNodeId) || null;
  }, [selectedNodeId, nodes]);

  // Empty state
  if (nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-32 text-gray-500 dark:text-gray-400">
        <span>No merge activity</span>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="relative w-full overflow-x-auto overflow-y-hidden bg-gray-50 dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-700"
      style={{ maxHeight: svgHeight + 20 }}
    >
      <svg
        ref={svgRef}
        width={svgWidth}
        height={svgHeight}
        viewBox={`0 0 ${svgWidth} ${svgHeight}`}
        className="block"
      >
        {/* Definitions for gradients and filters */}
        <defs>
          {/* Glow filter for active nodes */}
          <filter id="glow" x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="3" result="coloredBlur" />
            <feMerge>
              <feMergeNode in="coloredBlur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>

          {/* Arrow marker for connections */}
          <marker
            id="arrowhead"
            markerWidth="10"
            markerHeight="7"
            refX="9"
            refY="3.5"
            orient="auto"
          >
            <polygon points="0 0, 10 3.5, 0 7" fill="#4b5563" />
          </marker>
        </defs>

        {/* CSS animations for node entry */}
        <style>
          {`
            @keyframes nodeEntry {
              0% {
                opacity: 0;
                transform: scale(0.5) translateY(-10px);
              }
              60% {
                opacity: 1;
                transform: scale(1.1) translateY(0);
              }
              100% {
                opacity: 1;
                transform: scale(1) translateY(0);
              }
            }
            @keyframes branchEntry {
              0% {
                opacity: 0;
                transform: scale(0.8) translateY(-15px);
              }
              100% {
                opacity: 1;
                transform: scale(1) translateY(0);
              }
            }
          `}
        </style>

        {/* Background grid (subtle) */}
        <pattern id="grid" width="20" height="20" patternUnits="userSpaceOnUse">
          <path
            d="M 20 0 L 0 0 0 20"
            fill="none"
            stroke="#e5e7eb"
            strokeWidth="0.5"
            className="dark:stroke-gray-800"
          />
        </pattern>
        <rect width="100%" height="100%" fill="url(#grid)" opacity="0.3" />

        {/* Render connections first (behind nodes) */}
        <g className="connections">
          {connections.map((connection) => (
            <ConnectionLine
              key={connection.id}
              connection={connection}
              isNew={animationState.newConnectionIds.has(connection.id)}
            />
          ))}
        </g>

        {/* Render nodes */}
        <g className="nodes">
          {nodes.map((node) => (
            <TreeNodeComponent
              key={node.id}
              node={node}
              layout={layout}
              isHovered={hoveredNodeId === node.taskId}
              isNew={animationState.newNodeIds.has(node.id)}
              onClick={() => handleNodeClick(node.taskId, node)}
              onMouseEnter={() => handleDebouncedHover(node.taskId, node.x, node.y)}
              onMouseLeave={() => handleDebouncedHover(null, 0, 0)}
            />
          ))}
        </g>

        {/* Trunk label */}
        <text
          x={layout.padding - 5}
          y={layout.trunkY + 4}
          fontSize="10"
          fill="#6b7280"
          textAnchor="end"
          className="font-medium"
        >
          main
        </text>
      </svg>

      {/* Enhanced Tooltip */}
      {tooltipState.visible && tooltipState.nodeId && !selectedNodeId && (
        (() => {
          const hoveredNode = nodes.find((n) => n.taskId === tooltipState.nodeId);
          if (!hoveredNode) return null;
          return (
            <EnhancedTooltip
              node={hoveredNode}
              details={getNodeDetails?.(tooltipState.nodeId) || null}
              x={tooltipState.x}
              y={tooltipState.y}
              containerRef={containerRef as React.RefObject<HTMLDivElement>}
            />
          );
        })()
      )}

      {/* Detail Panel Modal */}
      {selectedNode && (
        <DetailPanel
          node={selectedNode}
          details={getNodeDetails?.(selectedNode.taskId) || null}
          onClose={handleCloseDetailPanel}
          onViewAgent={handleViewAgent}
        />
      )}
    </div>
  );
};

export default MergeQueueTree;
