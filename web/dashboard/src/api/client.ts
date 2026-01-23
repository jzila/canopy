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
  return fetchJson<RepositoryListResponse>('/api/daemon/repositories');
}

export async function activateRepository(repoId: string): Promise<Repository> {
  return fetchJson<Repository>(`/api/daemon/repositories/${repoId}/activate`, {
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
  // Watch mode fields
  watch_mode?: boolean;
  watch_iterations?: number;
  watch_tasks_total?: number;
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
  const endpoint = queryString ? `/api/daemon/runs?${queryString}` : '/api/daemon/runs';
  return fetchJson<RunListResponse>(endpoint);
}

// Orchestration types - matches Go backend (pkg/daemon/orchestration_handler.go)
export interface StartRunRequest {
  work_dir: string;
  repo_id?: string;
  concurrency?: number;
  max_priority?: number;
  use_bwrap?: boolean;
  max_retries?: number;
}

export interface StartRunResponse {
  success: boolean;
  run_id?: string;
  error?: string;
}

export interface StopRunResponse {
  success: boolean;
  error?: string;
}

export interface ActiveRunStatus {
  id: string;
  repo_path: string;
  repo_id?: string;
  status: string;
  start_time: number;
  end_time?: number;
  error?: string;
  tasks_total: number;
  tasks_done: number;
  tasks_failed: number;
}

export interface RunStatusResponse {
  success: boolean;
  run?: ActiveRunStatus;
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
  return fetchJson<StopRunResponse>('/api/orchestrator/run/stop', {
    method: 'POST',
    body: JSON.stringify({ run_id: runId }),
  });
}

export async function getRunStatus(runId?: string): Promise<RunStatusResponse> {
  const endpoint = runId ? `/api/orchestrator/run?id=${runId}` : '/api/orchestrator/run';
  return fetchJson<RunStatusResponse>(endpoint);
}

// Rules types - matches Go backend (pkg/config/config.go and pkg/rules/engine.go)

// ConfigRulesSettings for config-based rule settings
export interface ConfigRulesSettings {
  priority_min: number;
  priority_max: number;
  types?: string[];
  exclude_types?: string[];
  exclude_labels?: string[];
  assignee?: string;
  max_concurrent: number;
}

// UpdateConfigRequest for updating config settings
export interface UpdateConfigRequest {
  priority_min?: number;
  priority_max?: number;
  types?: string[];
  exclude_types?: string[];
  exclude_labels?: string[];
  assignee?: string;
}

// Unified Rule interface - single format for all rules
export interface Rule {
  name: string;
  conditions: string[];
  action: 'deny' | 'allow';
  enabled: boolean;
  persisted: boolean;
}

// RulesState contains the unified rules list with list-level persisted flag
export interface RulesState {
  rules: Rule[];
  persisted: boolean;
}

// RulesResponse from GET /api/rules
export interface RulesResponse {
  rules: Rule[];
  persisted: boolean;
}

export interface AddRuleRequest {
  name: string;
  conditions: string[];
  action: 'deny' | 'allow';
  enabled?: boolean;
}

export interface AddRuleResponse {
  success: boolean;
  rule?: Rule;
  error?: string;
}

export interface UpdateRuleRequest {
  enabled?: boolean;
  conditions?: string[];
  action?: 'deny' | 'allow';
}

export interface UpdateRuleResponse {
  success: boolean;
  rule?: Rule;
  error?: string;
}

export interface DeleteRuleResponse {
  success: boolean;
  error?: string;
}

// SaveRulesRequest for POST /api/rules/save
export interface SaveRulesRequest {
  rules: Rule[];
}

export interface SaveRulesResponse {
  success: boolean;
  error?: string;
}

// Rules API functions
// New URL structure: /api/repos/:repo_id/rules/*
export async function getRules(repoPath: string): Promise<RulesResponse> {
  return fetchJson<RulesResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules`);
}

export async function addRule(repoPath: string, request: AddRuleRequest): Promise<AddRuleResponse> {
  return fetchJson<AddRuleResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules`, {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function updateRule(repoPath: string, name: string, request: UpdateRuleRequest): Promise<UpdateRuleResponse> {
  return fetchJson<UpdateRuleResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify(request),
  });
}

export async function deleteRule(repoPath: string, name: string): Promise<DeleteRuleResponse> {
  return fetchJson<DeleteRuleResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
}

export async function saveRules(repoPath: string, request: SaveRulesRequest): Promise<SaveRulesResponse> {
  return fetchJson<SaveRulesResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules/save`, {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// ReorderRuleRequest for POST /api/repos/:repo_id/rules/:name/reorder
export interface ReorderRuleRequest {
  position: number;
}

export interface ReorderRuleResponse {
  success: boolean;
  rules?: Rule[];
  persisted?: boolean;
  error?: string;
}

export async function reorderRule(repoPath: string, name: string, position: number): Promise<ReorderRuleResponse> {
  return fetchJson<ReorderRuleResponse>(`/api/repos/${encodeURIComponent(repoPath)}/rules/${encodeURIComponent(name)}/reorder`, {
    method: 'POST',
    body: JSON.stringify({ position }),
  });
}

// Configuration API types - matches Go backend pkg/config, pkg/sandbox, pkg/validation

// SandboxConfig from pkg/sandbox/config.go
export interface SandboxSettings {
  enabled: boolean;
  network: 'allow' | 'deny' | 'proxy';
}

export interface ResourceSettings {
  max_memory: string;
  max_processes: number;
  max_open_files: number;
  max_disk: string;
  timeout: string;
}

export interface ExtraPathSettings {
  read_only?: string[];
  copy_configs?: string[];
}

export interface PathSettings {
  read_only?: string[];
  copy_configs?: string[];
  cache_mounts?: string[];
  extra?: ExtraPathSettings;
}

export interface SecuritySettings {
  blocked?: string[];
}

export interface SandboxConfig {
  sandbox: SandboxSettings;
  resources: ResourceSettings;
  paths: PathSettings;
  security: SecuritySettings;
}

export interface SandboxConfigResponse {
  config: SandboxConfig | null;
  error?: string;
}

// ValidationConfig from pkg/validation/config.go
export interface ValidationStep {
  name: string;
  command: string;
  timeout?: string;
  required: boolean;
}

export interface ValidationSettings {
  enabled: boolean;
  strict: boolean;
  timeout: string;
  max_repair_attempts: number;
  steps: ValidationStep[];
}

export interface ValidationConfig {
  validation: ValidationSettings;
}

export interface ValidationConfigResponse {
  config: ValidationConfig | null;
  error?: string;
}

// RulesSettings from pkg/config/config.go
export interface RulesSettings {
  priority_min: number;
  priority_max: number;
  types?: string[];
  exclude_types?: string[];
  labels?: string[];
  exclude_labels?: string[];
  assignee?: string;
  stop_when_empty?: boolean;
  max_concurrent?: number;
  max_concurrent_per_type?: Record<string, number>;
  max_concurrent_per_label?: Record<string, number>;
}

export interface RulesSettingsResponse {
  settings: RulesSettings | null;
  error?: string;
}

// Config API functions
// New URL structure: /api/repos/:repo_id/config/*

export async function getSandboxConfig(repoPath: string): Promise<SandboxConfigResponse> {
  return fetchJson<SandboxConfigResponse>(`/api/repos/${encodeURIComponent(repoPath)}/config/sandbox`);
}

export async function getValidationConfig(repoPath: string): Promise<ValidationConfigResponse> {
  return fetchJson<ValidationConfigResponse>(`/api/repos/${encodeURIComponent(repoPath)}/config/validation`);
}

export async function getRulesSettings(repoPath: string): Promise<RulesSettingsResponse> {
  return fetchJson<RulesSettingsResponse>(`/api/repos/${encodeURIComponent(repoPath)}/config/rules`);
}

export { ApiError };
