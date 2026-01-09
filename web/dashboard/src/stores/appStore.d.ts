export type AgentStatus = 'starting' | 'running' | 'completed' | 'failed' | 'timed_out' | 'cancelled';
export interface OutputBuffer {
    stdout: string;
    stderr: string;
}
export interface TokenUsage {
    input_tokens: number;
    output_tokens: number;
    total_tokens: number;
    cost_usd: number;
}
export interface AgentState {
    id: string;
    task_id: string;
    task_title: string;
    status: AgentStatus;
    start_time: string;
    end_time: string | null;
    duration: number;
    output: OutputBuffer;
    token_usage: TokenUsage;
    exit_code: number;
    error: string;
    changes: number;
    commits: number;
}
export interface TaskState {
    id: string;
    title: string;
    status: string;
    agent_id: string;
    priority: number;
    dependencies: string[];
}
export interface Stats {
    total_tasks: number;
    completed_tasks: number;
    failed_tasks: number;
    running_tasks: number;
    total_tokens: number;
    total_cost_usd: number;
    total_duration: number;
    avg_duration: number;
    file_changes: number;
    git_commits: number;
}
export interface RuntimeState {
    agents: Record<string, AgentState>;
    tasks: Record<string, TaskState>;
    stats: Stats;
    is_paused: boolean;
    start_time: string;
}
interface AppState {
    connected: boolean;
    agents: Record<string, AgentState>;
    tasks: Record<string, TaskState>;
    stats: Stats;
    isPaused: boolean;
    selectedAgentId: string | null;
    setConnected: (connected: boolean) => void;
    updateAgent: (id: string, update: Partial<AgentState>) => void;
    syncState: (state: RuntimeState) => void;
    appendOutput: (agentId: string, output: string) => void;
    setSelectedAgent: (id: string | null) => void;
    setIsPaused: (paused: boolean) => void;
}
export declare const useAppStore: import("zustand").UseBoundStore<import("zustand").StoreApi<AppState>>;
export {};
//# sourceMappingURL=appStore.d.ts.map