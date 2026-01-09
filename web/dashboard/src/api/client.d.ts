import type { RuntimeState, AgentState, TaskState, Stats } from '../stores/appStore';
declare class ApiError extends Error {
    status?: number | undefined;
    response?: unknown | undefined;
    constructor(message: string, status?: number | undefined, response?: unknown | undefined);
}
export declare function getState(): Promise<RuntimeState>;
export declare function getAgents(): Promise<AgentState[]>;
export declare function killAgent(id: string): Promise<{
    status: string;
    agent_id: string;
}>;
export declare function getTasks(): Promise<TaskState[]>;
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
export declare function createTask(title: string, priority?: number): Promise<CreateTaskResponse>;
export interface UpdateTaskRequest {
    status?: string;
}
export interface UpdateTaskResponse {
    id: string;
    status: string;
}
export declare function updateTask(id: string, status: string): Promise<UpdateTaskResponse>;
export declare function pauseOrch(): Promise<{
    status: string;
}>;
export declare function resumeOrch(): Promise<{
    status: string;
}>;
export declare function getStats(): Promise<Stats>;
export { ApiError };
//# sourceMappingURL=client.d.ts.map