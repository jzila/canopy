import { create } from 'zustand';
// Initial stats
const initialStats = {
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
export const useStateStore = create((set) => ({
    // Initial state
    connected: false,
    agents: {},
    tasks: {},
    stats: initialStats,
    isPaused: false,
    selectedAgentId: null,
    // Actions
    setConnected: (connected) => set({ connected }),
    updateAgent: (id, update) => set((state) => {
        const agent = state.agents[id];
        if (!agent)
            return state;
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
        if (!agent)
            return state;
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
//# sourceMappingURL=stateStore.js.map