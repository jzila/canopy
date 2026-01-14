/**
 * Types for the Merge Queue Tree Visualization
 */

// Status types for merge queue items
export type MergeStatus = 'pending' | 'acquiring' | 'merging' | 'resolving' | 'merged' | 'failed';

// A completed merge in the queue history
export interface CompletedMerge {
  taskId: string;
  agentId: string;
  timestamp: string;
  success: boolean;
  error?: string;
}

// A resolver branch spawned to handle merge conflicts
export interface ResolverBranch {
  parentTaskId: string;
  resolverTaskId: string;
  parentAgentId: string;
  resolverAgentId: string;
  status: 'resolving' | 'resolved' | 'failed';
}

// A pending merge waiting in the queue
export interface PendingMerge {
  taskId: string;
  agentId: string;
  position: number;
}

// An active worker currently working on a task
export interface ActiveWorker {
  agentId: string;
  taskId: string;
  status: MergeStatus;
}

// Props for the MergeQueueTree component
export interface MergeQueueTreeProps {
  completed: CompletedMerge[];
  resolvers: ResolverBranch[];
  pending: PendingMerge[];
  activeWorkers: ActiveWorker[];
  onNodeClick?: (taskId: string) => void;
  onNodeHover?: (taskId: string | null) => void;
}

// Internal node representation for rendering
export interface TreeNode {
  id: string;
  taskId: string;
  agentId: string;
  type: 'completed' | 'pending' | 'active' | 'resolver';
  status: 'success' | 'failed' | 'resolving' | 'resolved' | 'pending' | 'active';
  x: number;
  y: number;
  label?: string;
  parentId?: string;
  error?: string;
}

// Connection line between nodes
export interface TreeConnection {
  id: string;
  fromNode: string;
  toNode: string;
  type: 'trunk' | 'branch' | 'merge';
  fromX: number;
  fromY: number;
  toX: number;
  toY: number;
}

// Layout configuration
export interface TreeLayout {
  nodeRadius: number;
  nodeSpacing: number;
  trunkY: number;
  branchOffset: number;
  padding: number;
}
