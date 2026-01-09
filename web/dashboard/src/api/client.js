const API_BASE = 'http://localhost:8080';
class ApiError extends Error {
    constructor(message, status, response) {
        super(message);
        this.status = status;
        this.response = response;
        this.name = 'ApiError';
    }
}
async function fetchJson(endpoint, options) {
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
            throw new ApiError(`HTTP ${response.status}: ${errorText}`, response.status, errorText);
        }
        return await response.json();
    }
    catch (error) {
        if (error instanceof ApiError) {
            throw error;
        }
        throw new ApiError(`Network error: ${error instanceof Error ? error.message : String(error)}`);
    }
}
export async function getState() {
    return fetchJson('/api/state');
}
export async function getAgents() {
    return fetchJson('/api/agents');
}
export async function killAgent(id) {
    return fetchJson(`/api/agents/${id}/kill`, {
        method: 'POST',
    });
}
export async function getTasks() {
    return fetchJson('/api/tasks');
}
export async function createTask(title, priority) {
    const body = {
        title,
        ...(priority !== undefined && { priority }),
    };
    return fetchJson('/api/tasks', {
        method: 'POST',
        body: JSON.stringify(body),
    });
}
export async function updateTask(id, status) {
    const body = {
        status,
    };
    return fetchJson(`/api/tasks?id=${id}`, {
        method: 'PATCH',
        body: JSON.stringify(body),
    });
}
export async function pauseOrch() {
    return fetchJson('/api/orch/pause', {
        method: 'POST',
    });
}
export async function resumeOrch() {
    return fetchJson('/api/orch/resume', {
        method: 'POST',
    });
}
export async function getStats() {
    return fetchJson('/api/stats');
}
export { ApiError };
//# sourceMappingURL=client.js.map