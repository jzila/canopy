/**
 * Types for the Merge Queue Status Visualization
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

// Token usage information
export interface TokenUsage {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_usd: number;
}

// Git commit information
export interface GitCommit {
  hash: string;
  short_hash: string;
  message: string;
  author: string;
  author_email: string;
  timestamp: string;
  files_changed: string[];
}

// Detailed information for a merge item (from agent state)
export interface NodeDetails {
  title?: string;
  duration?: number;
  tokenUsage?: TokenUsage;
  filesChanged?: number;
  commitCount?: number;
  commits?: GitCommit[];
  output?: {
    stdout: string;
    stderr: string;
  };
}
