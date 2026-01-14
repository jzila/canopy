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
  trunkY: 60,
  branchOffset: 50,
  padding: 40,
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

  return (
    <g
      className={`tree-node cursor-pointer transition-transform ${isHovered ? 'scale-110' : ''}`}
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

  return (
    <path
      d={getPath()}
      fill="none"
      stroke={style.stroke}
      strokeWidth={style.strokeWidth}
      strokeDasharray={style.strokeDasharray}
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
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const layout = DEFAULT_LAYOUT;

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
