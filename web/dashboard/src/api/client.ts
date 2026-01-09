import type { RuntimeState, AgentState, TaskState, Stats } from '../stores/appStore';

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
}

export interface UpdateTaskResponse {
  id: string;
  status: string;
}

export async function updateTask(
  id: string,
  status: string
): Promise<UpdateTaskResponse> {
  const body: UpdateTaskRequest = {
    status,
  };

  return fetchJson<UpdateTaskResponse>(`/api/tasks?id=${id}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  });
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

export { ApiError };
