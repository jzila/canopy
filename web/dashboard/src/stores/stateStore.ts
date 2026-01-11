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

export interface LiveFeedEvent {
  id: string;
  timestamp: string;
  event_type: 'tool_use' | 'file_change' | 'text' | 'tool_result' | 'error';
  data: Record<string, unknown>;
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
  liveFeed: LiveFeedEvent[];
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
interface StateStore {
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
  appendOutput: (agentId: string, output: string, isError?: boolean) => void;
  appendLiveFeedEvent: (agentId: string, event: LiveFeedEvent) => void;
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

// Helper function to recalculate stats from agents
function recalculateStats(agents: Record<string, AgentState>): Stats {
  const stats: Stats = { ...initialStats };
  let totalDuration = 0;
  let completedCount = 0;

  for (const agent of Object.values(agents)) {
    switch (agent.status) {
      case 'completed':
        stats.completed_tasks++;
        completedCount++;
        totalDuration += agent.duration;
        break;
      case 'failed':
      case 'timed_out':
        stats.failed_tasks++;
        break;
      case 'running':
      case 'starting':
        stats.running_tasks++;
        break;
    }

    stats.total_tokens += agent.token_usage.total_tokens;
    stats.total_cost_usd += agent.token_usage.cost_usd;
    stats.file_changes += agent.changes;
    stats.git_commits += agent.commits;
  }

  stats.total_tasks = Object.keys(agents).length;
  stats.total_duration = totalDuration;
  if (completedCount > 0) {
    stats.avg_duration = totalDuration / completedCount;
  }

  return stats;
}

export const useStateStore = create<StateStore>((set) => ({
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
      const existingAgent = state.agents[id];

      // If agent doesn't exist, create it with the update data
      if (!existingAgent) {
        // Only create if we have the required id field
        if (!update.id) return state;

        const newAgents = {
          ...state.agents,
          [id]: update as AgentState,
        };
        return {
          agents: newAgents,
          stats: recalculateStats(newAgents),
        };
      }

      const newAgents = {
        ...state.agents,
        [id]: {
          ...existingAgent,
          ...update,
        },
      };
      return {
        agents: newAgents,
        stats: recalculateStats(newAgents),
      };
    }),

  syncState: (runtimeState) =>
    set({
      agents: runtimeState.agents,
      tasks: runtimeState.tasks,
      stats: runtimeState.stats,
      isPaused: runtimeState.is_paused,
    }),

  appendOutput: (agentId, output, isError = false) =>
    set((state) => {
      const agent = state.agents[agentId];
      if (!agent) return state;

      return {
        agents: {
          ...state.agents,
          [agentId]: {
            ...agent,
            output: {
              stdout: isError ? agent.output.stdout : agent.output.stdout + output,
              stderr: isError ? agent.output.stderr + output : agent.output.stderr,
            },
          },
        },
      };
    }),

  appendLiveFeedEvent: (agentId, event) =>
    set((state) => {
      const agent = state.agents[agentId];
      if (!agent) return state;

      // Limit feed to last 500 events to prevent memory issues
      const maxEvents = 500;
      const existingEvents = agent.liveFeed || [];
      const newEvents = [...existingEvents, event].slice(-maxEvents);

      return {
        agents: {
          ...state.agents,
          [agentId]: {
            ...agent,
            liveFeed: newEvents,
          },
        },
      };
    }),

  setSelectedAgent: (selectedAgentId) => set({ selectedAgentId }),

  setIsPaused: (isPaused) => set({ isPaused }),
}));
