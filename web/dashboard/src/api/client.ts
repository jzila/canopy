import type { RuntimeState, AgentState, TaskState, Stats } from '../stores/stateStore';

const API_BASE = 'http://localhost:8080';

class ApiError extends Error {
  constructor(
    message: string,
    public status?: number,
    public response?: unknown
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function fetchJson<T>(
  endpoint: string,
  options?: RequestInit
): Promise<T> {
  const url = `${API_BASE}${endpoint}`;

  try {
    const response = await fetch(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new ApiError(
        `HTTP ${response.status}: ${errorText}`,
        response.status,
        errorText
      );
    }

    return await response.json();
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }
    throw new ApiError(
      `Network error: ${error instanceof Error ? error.message : String(error)}`
    );
  }
}

export async function getState(): Promise<RuntimeState> {
  return fetchJson<RuntimeState>('/api/state');
}

export async function getAgents(): Promise<AgentState[]> {
  return fetchJson<AgentState[]>('/api/agents');
}

export async function killAgent(id: string): Promise<{ status: string; agent_id: string }> {
  return fetchJson<{ status: string; agent_id: string }>(`/api/agents/${id}/kill`, {
    method: 'POST',
  });
}

export interface UpdateAgentRequest {
  archived?: boolean;
}

export interface UpdateAgentResponse {
  id: string;
  archived?: boolean;
}

export async function updateAgent(
  id: string,
  updates: UpdateAgentRequest
): Promise<UpdateAgentResponse> {
  return fetchJson<UpdateAgentResponse>(`/api/agents?id=${id}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export async function archiveAgent(
  id: string,
  archived: boolean
): Promise<UpdateAgentResponse> {
  return updateAgent(id, { archived });
}

export async function getTasks(): Promise<TaskState[]> {
  return fetchJson<TaskState[]>('/api/tasks');
}

export interface CreateTaskRequest {
  title: string;
  description?: string;
  priority?: number;
  dependencies?: string[];
}

export interface CreateTaskResponse {
  id: string;
  title: string;
  status: string;
}

export async function createTask(
  title: string,
  priority?: number
): Promise<CreateTaskResponse> {
  const body: CreateTaskRequest = {
    title,
    ...(priority !== undefined && { priority }),
  };

  return fetchJson<CreateTaskResponse>('/api/tasks', {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export interface UpdateTaskRequest {
  status?: string;
  archived?: boolean;
}

export interface UpdateTaskResponse {
  id: string;
  status?: string;
  archived?: boolean;
}

export async function updateTask(
  id: string,
  updates: UpdateTaskRequest
): Promise<UpdateTaskResponse> {
  return fetchJson<UpdateTaskResponse>(`/api/tasks?id=${id}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export async function archiveTask(
  id: string,
  archived: boolean
): Promise<UpdateTaskResponse> {
  return updateTask(id, { archived });
}

export async function pauseOrch(): Promise<{ status: string }> {
  return fetchJson<{ status: string }>('/api/orch/pause', {
    method: 'POST',
  });
}

export async function resumeOrch(): Promise<{ status: string }> {
  return fetchJson<{ status: string }>('/api/orch/resume', {
    method: 'POST',
  });
}

export async function getStats(): Promise<Stats> {
  return fetchJson<Stats>('/api/stats');
}

// Repository types
export interface Repository {
  id: string;
  path: string;
  name: string;
  created_at: string;
  is_active: boolean;
}

export interface RepositoryListResponse {
  repositories: Repository[];
  active_repo_id: string;
}

export async function getRepositories(): Promise<RepositoryListResponse> {
  return fetchJson<RepositoryListResponse>('/api/repositories');
}

export async function activateRepository(repoId: string): Promise<Repository> {
  return fetchJson<Repository>(`/api/repositories/${repoId}/activate`, {
    method: 'POST',
  });
}

// Merge Queue types
export interface MergeCompletedItem {
  task_id: string;
  agent_id: string;
  timestamp: string;
  success: boolean;
  error?: string;
}

export interface MergeResolverItem {
  parent_task_id: string;
  resolver_task_id: string;
  parent_agent_id: string;
  resolver_agent_id: string;
  status: 'running' | 'completed' | 'failed';
}

export interface MergePendingItem {
  task_id: string;
  agent_id: string;
  position: number;
}

export interface MergeWorkerItem {
  agent_id: string;
  task_id: string;
  status: string;
}

export interface MergeQueueState {
  completed: MergeCompletedItem[];
  resolvers: MergeResolverItem[];
  pending: MergePendingItem[];
  active_workers: MergeWorkerItem[];
  is_paused: boolean;
  queue_length: number;
}

export async function getMergeQueue(): Promise<MergeQueueState> {
  return fetchJson<MergeQueueState>('/api/merge-queue');
}

// Run types for historical run data
export interface Run {
  id: string;
  started_at: string;
  finished_at?: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'partial';
  total_tasks: number;
  completed_tasks: number;
  failed_tasks: number;
  repo_id?: string;
  repo_path?: string;
  repo_name?: string;
  total_cost_usd: number;
  duration_seconds: number;
}

export interface RunListResponse {
  runs: Run[];
  pagination: {
    total: number;
    limit: number;
    offset: number;
  };
}

export interface RunListFilter {
  repo_id?: string;
  status?: string;
  limit?: number;
  offset?: number;
}

export async function getRuns(filter?: RunListFilter): Promise<RunListResponse> {
  const params = new URLSearchParams();
  if (filter?.repo_id) params.set('repo_id', filter.repo_id);
  if (filter?.status) params.set('status', filter.status);
  if (filter?.limit) params.set('limit', String(filter.limit));
  if (filter?.offset) params.set('offset', String(filter.offset));

  const queryString = params.toString();
  const endpoint = queryString ? `/api/runs?${queryString}` : '/api/runs';
  return fetchJson<RunListResponse>(endpoint);
}

// Orchestrator run control types
export interface StartRunRequest {
  work_dir: string;
  output_dir?: string;
  concurrency?: number;
  verbose?: boolean;
  dry_run?: boolean;
  use_bwrap?: boolean;
  max_retries?: number;
  max_priority?: number;
  resolver_timeout_ms?: number;
  repo_id?: string;
}

export interface StartRunResponse {
  success: boolean;
  run_id?: string;
  error?: string;
}

export interface StopRunRequest {
  run_id: string;
}

export interface StopRunResponse {
  success: boolean;
  error?: string;
}

export interface ActiveRunStatus {
  id: string;
  repo_path: string;
  repo_id?: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  start_time: number;
  end_time?: number;
  error?: string;
  tasks_total: number;
  tasks_done: number;
  tasks_failed: number;
}

export interface ListActiveRunsResponse {
  success: boolean;
  runs?: ActiveRunStatus[];
  error?: string;
}

export async function startRun(request: StartRunRequest): Promise<StartRunResponse> {
  return fetchJson<StartRunResponse>('/api/orchestrator/run', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function stopRun(runId: string): Promise<StopRunResponse> {
  const request: StopRunRequest = { run_id: runId };
  return fetchJson<StopRunResponse>('/api/orchestrator/run/stop', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function getActiveRuns(): Promise<ListActiveRunsResponse> {
  return fetchJson<ListActiveRunsResponse>('/api/orchestrator/runs');
}

export async function getActiveRunStatus(runId: string): Promise<{ success: boolean; run?: ActiveRunStatus; error?: string }> {
  return fetchJson<{ success: boolean; run?: ActiveRunStatus; error?: string }>(`/api/orchestrator/runs/${runId}`);
}

export { ApiError };
