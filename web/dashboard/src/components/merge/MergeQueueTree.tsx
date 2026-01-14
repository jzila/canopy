import React, { useMemo, useRef, useEffect, useCallback, useState } from 'react';
import type {
  MergeQueueTreeProps,
  TreeNode,
  TreeConnection,
  TreeLayout,
  CompletedMerge,
  PendingMerge,
  ActiveWorker,
  ResolverBranch,
} from './types';

// Default layout configuration
const DEFAULT_LAYOUT: TreeLayout = {
  nodeRadius: 12,
  nodeSpacing: 80,
  trunkY: 120, // Increased to allow workers to fan out above and below
  branchOffset: 50,
  padding: 40,
};

// Spacing for worker fan-out branches
const WORKER_BRANCH_SPACING = 45; // Vertical space between worker branches
const WORKER_BRANCH_LENGTH = 60; // Horizontal length of worker branches

// Truncate ID for display
const truncateId = (id: string, length: number = 7): string => {
  return id.length > length ? id.slice(0, length) : id;
};

// Node component for rendering individual nodes
interface NodeProps {
  node: TreeNode;
  layout: TreeLayout;
  isHovered: boolean;
  onClick?: () => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
}

const TreeNodeComponent: React.FC<NodeProps> = ({
  node,
  layout,
  isHovered,
  onClick,
  onMouseEnter,
  onMouseLeave,
}) => {
  const { nodeRadius } = layout;
  const { x, y, type, status } = node;

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
  const isWorker = type === 'active';

  return (
    <g
      className={`tree-node cursor-pointer transition-transform ${isHovered ? 'scale-110' : ''} ${isWorker ? 'worker-node-animate' : ''}`}
      style={{ transformOrigin: `${x}px ${y}px` }}
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

      {/* Label below node */}
      <text
        x={x}
        y={y + nodeRadius + 16}
        textAnchor="middle"
        fontSize="10"
        fill="#9ca3af"
        className="font-mono"
      >
        {truncateId(node.taskId)}
      </text>

      {/* Hover tooltip */}
      {isHovered && (
        <g>
          <rect
            x={x - 60}
            y={y - nodeRadius - 40}
            width={120}
            height={28}
            rx={4}
            fill="#1f2937"
            stroke="#374151"
            strokeWidth={1}
          />
          <text
            x={x}
            y={y - nodeRadius - 22}
            textAnchor="middle"
            fontSize="11"
            fill="#f3f4f6"
            className="font-mono"
          >
            {node.taskId}
          </text>
        </g>
      )}
    </g>
  );
};

// Connection line component
interface ConnectionProps {
  connection: TreeConnection;
}

const ConnectionLine: React.FC<ConnectionProps> = ({ connection }) => {
  const { fromX, fromY, toX, toY, type } = connection;

  // Calculate control points for curved paths
  const getMidX = () => (fromX + toX) / 2;
  const getMidY = () => (fromY + toY) / 2;

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
      case 'worker':
        return {
          stroke: '#3b82f6', // blue-500
          strokeWidth: 2,
          strokeDasharray: undefined,
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
      // Curved path going down for branch (resolvers)
      const midX = getMidX();
      return `M ${fromX} ${fromY} C ${midX} ${fromY}, ${midX} ${toY}, ${toX} ${toY}`;
    } else if (type === 'worker') {
      // Curved path for worker fan-out branches
      // Starts horizontal, curves to the worker position
      const controlX1 = fromX + (toX - fromX) * 0.4;
      const controlX2 = fromX + (toX - fromX) * 0.6;
      return `M ${fromX} ${fromY} C ${controlX1} ${fromY}, ${controlX2} ${toY}, ${toX} ${toY}`;
    } else {
      // Curved path going up for merge
      const midX = getMidX();
      return `M ${fromX} ${fromY} C ${midX} ${fromY}, ${midX} ${toY}, ${toX} ${toY}`;
    }
  };

  return (
    <path
      d={getPath()}
      fill="none"
      stroke={style.stroke}
      strokeWidth={style.strokeWidth}
      strokeDasharray={style.strokeDasharray}
      strokeLinecap="round"
      className={`tree-connection transition-all duration-300 ${type === 'worker' ? 'worker-branch-animate' : ''}`}
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
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const layout = DEFAULT_LAYOUT;

  // Build nodes and connections from props
  const { nodes, connections, svgWidth, svgHeight, minY } = useMemo(() => {
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

    // Calculate the fork point X position (end of completed nodes on trunk)
    const forkPointX = layout.padding + completed.length * layout.nodeSpacing;

    // Add active workers as fan-out branches from the fork point
    // Workers are distributed vertically: above and below the trunk
    if (activeWorkers.length > 0) {
      // Calculate vertical positions for workers (fan out symmetrically)
      // Workers alternate: 0 goes above, 1 goes below, 2 goes further above, etc.
      const getWorkerY = (index: number): number => {
        const position = Math.floor(index / 2) + 1; // 1, 1, 2, 2, 3, 3...
        const isAbove = index % 2 === 0;
        return isAbove
          ? trunkY - position * WORKER_BRANCH_SPACING
          : trunkY + position * WORKER_BRANCH_SPACING;
      };

      activeWorkers.forEach((worker, index) => {
        const workerY = getWorkerY(index);
        const workerX = forkPointX + WORKER_BRANCH_LENGTH;
        const nodeId = `active-${worker.taskId}`;

        nodeList.push({
          id: nodeId,
          taskId: worker.taskId,
          agentId: worker.agentId,
          type: 'active',
          status: 'active',
          x: workerX,
          y: workerY,
        });

        // Connect worker to fork point with a curved branch
        connectionList.push({
          id: `worker-branch-${index}`,
          fromNode: 'fork-point',
          toNode: nodeId,
          type: 'worker',
          fromX: forkPointX,
          fromY: trunkY,
          toX: workerX - layout.nodeRadius,
          toY: workerY,
        });

        // Check for resolvers on active workers
        const workerResolvers = resolverMap.get(worker.taskId);
        if (workerResolvers) {
          workerResolvers.forEach((resolver, resolverIndex) => {
            // Position resolver branches extending from the worker
            const resolverY = workerY + (workerY >= trunkY ? 1 : -1) * (layout.branchOffset + resolverIndex * 40);
            const resolverX = workerX + layout.nodeSpacing / 2;
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
              fromX: workerX + layout.nodeRadius,
              fromY: workerY,
              toX: resolverX - layout.nodeRadius,
              toY: resolverY,
            });
          });
        }
      });
    }

    // Update current X for pending items (after the worker branch area)
    currentX = forkPointX + (activeWorkers.length > 0 ? WORKER_BRANCH_LENGTH + layout.nodeSpacing : 0);

    // Add pending nodes continuing on the trunk after the fork point
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

      // Connect to previous node (fork point or previous pending)
      let prevNodeId: string | null = null;
      let prevX: number;

      if (index === 0) {
        // First pending connects to the fork point (end of completed)
        const lastCompleted = completed[completed.length - 1];
        if (lastCompleted) {
          prevNodeId = `completed-${lastCompleted.taskId}`;
          prevX = forkPointX - layout.nodeSpacing + layout.nodeRadius;
        } else {
          prevX = layout.padding;
        }
      } else {
        const prevPending = pending[index - 1];
        if (prevPending) {
          prevNodeId = `pending-${prevPending.taskId}`;
        }
        prevX = x - layout.nodeSpacing + layout.nodeRadius;
      }

      if (prevNodeId) {
        connectionList.push({
          id: `trunk-pending-${index}`,
          fromNode: prevNodeId,
          toNode: nodeId,
          type: 'trunk',
          fromX: prevX,
          fromY: trunkY,
          toX: x - layout.nodeRadius,
          toY: trunkY,
        });
      }
    });

    // Calculate SVG dimensions
    // Width: completed nodes + worker branch area + pending nodes
    const workerBranchWidth = activeWorkers.length > 0 ? WORKER_BRANCH_LENGTH + layout.nodeSpacing : 0;
    const calculatedWidth = Math.max(
      layout.padding * 2 +
        completed.length * layout.nodeSpacing +
        workerBranchWidth +
        pending.length * layout.nodeSpacing,
      300
    );

    // Find min and max Y for height calculation (workers fan both above and below)
    const allYPositions = nodeList.map((n) => n.y);
    const minY = Math.min(layout.padding, ...allYPositions.map((y) => y - layout.nodeRadius - 30));
    const maxY = Math.max(
      trunkY + layout.nodeRadius + 30,
      ...allYPositions.map((y) => y + layout.nodeRadius + 30)
    );

    return {
      nodes: nodeList,
      connections: connectionList,
      svgWidth: calculatedWidth,
      svgHeight: maxY - minY + layout.padding * 2,
      // Store minY offset for viewBox adjustment
      minY,
    };
  }, [completed, resolvers, pending, activeWorkers, layout]);

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

  // Handle node interactions
  const handleNodeClick = useCallback(
    (taskId: string) => {
      onNodeClick?.(taskId);
    },
    [onNodeClick]
  );

  const handleNodeHover = useCallback(
    (taskId: string | null) => {
      setHoveredNodeId(taskId);
      onNodeHover?.(taskId);
    },
    [onNodeHover]
  );

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
      className="w-full overflow-x-auto overflow-y-hidden bg-gray-50 dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-700"
      style={{ maxHeight: svgHeight + 20 }}
    >
      <svg
        ref={svgRef}
        width={svgWidth}
        height={svgHeight}
        viewBox={`0 ${minY} ${svgWidth} ${svgHeight}`}
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
            <ConnectionLine key={connection.id} connection={connection} />
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
              onClick={() => handleNodeClick(node.taskId)}
              onMouseEnter={() => handleNodeHover(node.taskId)}
              onMouseLeave={() => handleNodeHover(null)}
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
    </div>
  );
};

export default MergeQueueTree;
