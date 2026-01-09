import { create } from 'zustand';

// Types based on Go backend structures

export type AgentStatus =
  | 'starting'
  | 'running'
  | 'completed'
  | 'failed'
  | 'timed_out'
  | 'cancelled';

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

// Store interface
interface AppState {
  // State
  connected: boolean;
  agents: Record<string, AgentState>;
  tasks: Record<string, TaskState>;
  stats: Stats;
  isPaused: boolean;
  selectedAgentId: string | null;

  // Actions
  setConnected: (connected: boolean) => void;
  updateAgent: (id: string, update: Partial<AgentState>) => void;
  syncState: (state: RuntimeState) => void;
  appendOutput: (agentId: string, output: string) => void;
  setSelectedAgent: (id: string | null) => void;
  setIsPaused: (paused: boolean) => void;
}

// Initial stats
const initialStats: Stats = {
  total_tasks: 0,
  completed_tasks: 0,
  failed_tasks: 0,
  running_tasks: 0,
  total_tokens: 0,
  total_cost_usd: 0,
  total_duration: 0,
  avg_duration: 0,
  file_changes: 0,
  git_commits: 0,
};

export const useAppStore = create<AppState>((set) => ({
  // Initial state
  connected: false,
  agents: {},
  tasks: {},
  stats: initialStats,
  isPaused: false,
  selectedAgentId: null,

  // Actions
  setConnected: (connected) => set({ connected }),

  updateAgent: (id, update) =>
    set((state) => {
      const agent = state.agents[id];
      if (!agent) return state;

      return {
        agents: {
          ...state.agents,
          [id]: {
            ...agent,
            ...update,
          },
        },
      };
    }),

  syncState: (runtimeState) => set({
    agents: runtimeState.agents,
    tasks: runtimeState.tasks,
    stats: runtimeState.stats,
    isPaused: runtimeState.is_paused,
  }),

  appendOutput: (agentId, output) => set((state) => {
    const agent = state.agents[agentId];
    if (!agent) return state;

    return {
      agents: {
        ...state.agents,
        [agentId]: {
          ...agent,
          output: {
            ...agent.output,
            stdout: agent.output.stdout + output,
          },
        },
      },
    };
  }),

  setSelectedAgent: (selectedAgentId) => set({ selectedAgentId }),

  setIsPaused: (isPaused) => set({ isPaused }),
}));
