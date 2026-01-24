import { create } from 'zustand';
import type { Repository, MergeQueueState, Run, ActiveRunStatus, Rule } from '../api/client';
import { getRunConfig } from '../api/client';

// Types based on Go backend structures

export type AgentStatus =
  | 'starting'
  | 'running'
  | 'completed'
  | 'failed'
  | 'timed_out'
  | 'cancelled';

// Merge status types matching Go backend (ipc/protocol.go)
export type MergeStatus = 'pending' | 'acquiring' | 'merging' | 'resolving' | 'merged' | 'failed' | 'skipped' | 'merged_needs_repair';

// Validation status types matching Go backend (validation/executor.go)
// Includes repair-related intermediate states: pending_repair (deciding to spawn), spawning_repair (creating agent), repairing (agent executing)
export type ValidationStatus = 'pending' | 'running' | 'passed' | 'failed' | 'skipped' | 'pending_repair' | 'spawning_repair' | 'repairing';

// Lifecycle state types matching Go backend (lifecycle/state.go)
// This is the new unified state machine that replaces interpreting multiple fields
export type LifecycleState =
  | 'starting'
  | 'running'
  | 'queued_for_merge'
  | 'merging'
  | 'resolving'
  | 'validating'
  | 'repairing'
  | 'merge_failed'
  | 'completed'
  | 'failed'
  | 'needs_attention'
  | 'cancelled'
  | 'timed_out';

// ValidationStep represents a single validation step result
export interface ValidationStep {
  name: string;           // e.g., "build", "test", "lint"
  status: string;         // pending, running, passed, failed, skipped
  duration_ms: number;    // Duration in milliseconds
  output?: string;        // Output or error message
}

// WorkerChainItem represents an item in the worker chain timeline
export interface WorkerChainItem {
  type: 'agent' | 'resolver' | 'validation' | 'repair';
  status: 'pending' | 'running' | 'success' | 'failed';
  duration_ms?: number;
  output?: string;
  attempt?: number;  // For repair agents
}

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
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  total_tokens: number;
  cost_usd: number;
}

export interface GitCommit {
  hash: string;
  short_hash: string;
  message: string;
  author: string;
  author_email: string;
  timestamp: string;
  files_changed: string[];
}

export interface AgentState {
  id: string;
  run_id?: string;             // ID of the run this agent belongs to
  task_id: string;
  task_title: string;
  task_description?: string;   // Task description for display
  parent_agent_id?: string;    // ID of parent agent if spawned by another agent
  child_agent_ids?: string[];  // IDs of child agents spawned by this agent
  status: AgentStatus;
  lifecycle_state?: LifecycleState; // New unified lifecycle state (optional for backwards compat)
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
  git_commits: GitCommit[];
  result_message?: string;    // Final result message from agent
  archived: boolean;
  // Retry information
  attempt?: number;           // Current attempt number (1 = first try, 2 = first retry, etc.)
  max_retries?: number;       // Maximum retry attempts configured (0 = no retries, -1 = infinite)
  // Merge status fields (snake_case per API conventions)
  merge_status?: MergeStatus;
  merge_queue_pos?: number;
  merge_error?: string;
  merge_commits_applied?: number;   // Number of commits applied during merge
  merge_had_conflict?: boolean;     // Whether merge had conflicts
  merge_resolver_spawned?: boolean; // Whether resolver agent was spawned
  // Validation and repair fields
  validation_status?: ValidationStatus;      // Overall validation status
  validation_steps?: ValidationStep[];       // Results of individual validation steps
  validation_duration_ms?: number;           // Total validation duration in milliseconds
  validation_error?: string;                 // Error message if validation failed
  repair_attempts?: number;                  // Number of repair attempts made (0 = no repairs)
  last_repair_output?: string;               // Output/error from last repair attempt
  worker_chain?: WorkerChainItem[];          // Full worker chain timeline
}

export interface TaskState {
  id: string;
  title: string;
  status: string;
  type?: string;  // Task type (task, bug, feature, etc.)
  agent_id: string;
  priority: number;
  dependencies: string[];
  archived: boolean;
  updated_at?: number;  // Unix timestamp of last update
}

export interface Stats {
  total_tasks: number;
  completed_tasks: number;
  failed_tasks: number;
  running_tasks: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_creation_tokens: number;
  total_cache_read_tokens: number;
  total_tokens: number;
  total_cost_usd: number;
  total_duration: number;
  avg_duration: number;
  file_changes: number;
  git_commits: number;
  all_git_commits: GitCommit[];
}

// Pause state enum matching Go backend (ipc/protocol.go)
export type PauseState = 'running' | 'paused_user' | 'paused_agent' | 'paused_both';

// Orchestrator state enum matching Go backend (daemon/orchestrator_manager.go)
export type OrchestratorState = 'off' | 'idle' | 'active' | 'paused';

// Run configuration for starting new runs
export interface RunConfig {
  concurrency: number;
  max_priority: number;
  use_bwrap: boolean;
  max_retries: number;
}

// Default run configuration
export const DEFAULT_RUN_CONFIG: RunConfig = {
  concurrency: 4,
  max_priority: 4,
  use_bwrap: true,
  max_retries: 3,
};

// Optimistic update types
export type OptimisticOperationType =
  | 'start_run'
  | 'stop_run'
  | 'pause_orch'
  | 'resume_orch';

export interface OptimisticOperation {
  id: string;
  type: OptimisticOperationType;
  previousState: Partial<OptimisticStateSnapshot>;
  timestamp: number;
}

// Snapshot of state fields that can be optimistically updated
export interface OptimisticStateSnapshot {
  orchestratorState: OrchestratorState;
  pauseState: PauseState;
  isPaused: boolean;
  isPausedByUser: boolean;
  isPausedByAgent: boolean;
  currentRunId: string;
  isStartingRun: boolean;
  isStoppingRun: boolean;
}

export interface RuntimeState {
  agents: Record<string, AgentState>;
  tasks: Record<string, TaskState>;
  stats: Stats;
  is_paused: boolean;
  is_paused_by_user: boolean;
  is_paused_by_agent: boolean;
  pause_state: PauseState;
  start_time: string;
  current_run_id: string;
  orchestrator_state: OrchestratorState;
  active_agent_count: number;
  // Event sequence number for ordering - events with sequence <= this are already
  // reflected in the snapshot and should be discarded by the client
  event_sequence?: number;
}

// Store interface
interface StateStore {
  // State
  connected: boolean;
  agents: Record<string, AgentState>;
  tasks: Record<string, TaskState>;
  stats: Stats;
  isPaused: boolean;
  isPausedByUser: boolean;
  isPausedByAgent: boolean;
  pauseState: PauseState;
  currentRunId: string; // Currently active orchestrator run ID (empty if no active run)
  orchestratorState: OrchestratorState; // Current orchestrator state (off/idle/active/paused)
  activeAgentCount: number; // Number of currently running agents
  // Event sequence number from last state:sync - used to discard stale events
  // that occurred before the snapshot was taken
  eventSequence: number;
  selectedAgentId: string | null;
  highlightedTaskId: string | null;
  selectedBeadId: string | null; // Selected bead for filtering agents
  repositories: Repository[];
  activeRepoId: string;
  isRepoSwitching: boolean;
  mergeQueue: MergeQueueState | null;
  // Run filtering state
  runs: Run[];
  activeRunId: string; // empty string means "All runs" / current run
  isRunsLoading: boolean;
  // Run control state
  isStartingRun: boolean;
  isStoppingRun: boolean;
  runConfig: RunConfig;
  showRunConfigDialog: boolean;
  activeOrchestratorRun: ActiveRunStatus | null; // Currently running orchestrator run
  // Rules state - unified format
  rules: Rule[];
  rulesPersistedState: boolean; // list-level persisted flag
  isRulesLoading: boolean;
  showAddRuleDialog: boolean;

  // Optimistic update state
  pendingOperations: OptimisticOperation[];
  lastError: { operation: OptimisticOperationType; message: string } | null;

  // Actions
  setConnected: (connected: boolean) => void;
  updateAgent: (id: string, update: Partial<AgentState>) => void;
  updateTask: (id: string, update: Partial<TaskState>) => void;
  syncState: (state: RuntimeState) => void;
  appendOutput: (agentId: string, output: string, isError?: boolean) => void;
  clearOutput: (agentId: string) => void;
  appendLiveFeedEvent: (agentId: string, event: LiveFeedEvent) => void;
  appendGitCommit: (agentId: string, commit: GitCommit) => void;
  setSelectedAgent: (id: string | null) => void;
  setHighlightedTask: (id: string | null) => void;
  setSelectedBead: (id: string | null) => void;
  setPauseState: (
    isPaused: boolean,
    isPausedByUser: boolean,
    isPausedByAgent: boolean,
    pauseState: PauseState
  ) => void;
  setRepositories: (repositories: Repository[], activeRepoId: string) => void;
  setActiveRepo: (repoId: string) => void;
  setRepoSwitching: (isSwitching: boolean) => void;
  setMergeQueue: (queue: MergeQueueState) => void;
  updateAgentMergeStatus: (
    agentId: string,
    mergeStatus: MergeStatus,
    queuePos?: number,
    error?: string,
    commitsApplied?: number,
    validationStatus?: ValidationStatus,
    validationSteps?: ValidationStep[],
    validationDurationMs?: number,
    validationError?: string,
    repairAttempts?: number,
    lastRepairOutput?: string
  ) => void;
  // Current run tracking
  setCurrentRunId: (runId: string) => void;
  // Orchestrator state
  setOrchestratorState: (state: OrchestratorState, activeAgentCount: number) => void;
  // Run filtering actions
  setRuns: (runs: Run[]) => void;
  setActiveRunId: (runId: string) => void;
  setRunsLoading: (loading: boolean) => void;
  addRun: (run: Run) => void;
  updateRun: (runId: string, update: Partial<Run>) => void;
  // Run control actions
  setStartingRun: (starting: boolean) => void;
  setStoppingRun: (stopping: boolean) => void;
  setRunConfig: (config: Partial<RunConfig>) => void;
  setShowRunConfigDialog: (show: boolean) => void;
  setActiveOrchestratorRun: (run: ActiveRunStatus | null) => void;
  updateActiveOrchestratorRun: (update: Partial<ActiveRunStatus>) => void;
  // Rules actions - unified format
  setRulesState: (rules: Rule[], persisted: boolean) => void;
  setRulesLoading: (loading: boolean) => void;
  setShowAddRuleDialog: (show: boolean) => void;
  addRule: (rule: Rule) => void;
  updateRule: (name: string, update: Partial<Rule>) => void;
  removeRule: (name: string) => void;
  reorderRules: (fromIndex: number, toIndex: number) => void;

  // Optimistic update actions
  startOptimisticRun: (tempRunId: string) => void;
  confirmRunStarted: (runId: string) => void;
  rollbackRunStart: (error: string) => void;

  startOptimisticStop: () => void;
  confirmRunStopped: () => void;
  rollbackRunStop: (error: string) => void;

  startOptimisticPause: () => void;
  confirmPause: (pauseState: PauseState) => void;
  rollbackPause: (error: string) => void;

  startOptimisticResume: () => void;
  confirmResume: (pauseState: PauseState) => void;
  rollbackResume: (error: string) => void;

  clearLastError: () => void;

  // Config persistence actions
  loadSavedConfig: () => Promise<void>;
  updateRunConfigFromServer: (config: RunConfig) => void;
}

// Initial stats
const initialStats: Stats = {
  total_tasks: 0,
  completed_tasks: 0,
  failed_tasks: 0,
  running_tasks: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost_usd: 0,
  total_duration: 0,
  avg_duration: 0,
  file_changes: 0,
  git_commits: 0,
  all_git_commits: [],
};

// Helper function to recalculate stats from agents
function recalculateStats(agents: Record<string, AgentState>): Stats {
  const stats: Stats = { ...initialStats, all_git_commits: [] };
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

    // Track all token types separately
    stats.total_input_tokens += agent.token_usage.input_tokens;
    stats.total_output_tokens += agent.token_usage.output_tokens;
    stats.total_cache_creation_tokens += agent.token_usage.cache_creation_input_tokens ?? 0;
    stats.total_cache_read_tokens += agent.token_usage.cache_read_input_tokens ?? 0;
    stats.total_tokens += agent.token_usage.total_tokens;
    stats.total_cost_usd += agent.token_usage.cost_usd;
    stats.file_changes += agent.changes;
    stats.git_commits += agent.commits;

    // Aggregate git commits from all agents
    if (agent.git_commits && agent.git_commits.length > 0) {
      stats.all_git_commits.push(...agent.git_commits);
    }
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
  isPausedByUser: false,
  isPausedByAgent: false,
  pauseState: 'running',
  currentRunId: '', // empty means no active orchestrator run
  orchestratorState: 'off' as OrchestratorState,
  activeAgentCount: 0,
  eventSequence: 0, // Sequence number from last state:sync
  selectedAgentId: null,
  highlightedTaskId: null,
  selectedBeadId: null,
  repositories: [],
  activeRepoId: '',
  isRepoSwitching: false,
  mergeQueue: null,
  // Run filtering state
  runs: [],
  activeRunId: '', // empty means "All runs"
  isRunsLoading: false,
  // Run control state
  isStartingRun: false,
  isStoppingRun: false,
  runConfig: DEFAULT_RUN_CONFIG,
  showRunConfigDialog: false,
  activeOrchestratorRun: null,
  // Rules state - unified format
  rules: [],
  rulesPersistedState: false,
  isRulesLoading: false,
  showAddRuleDialog: false,

  // Optimistic update state
  pendingOperations: [],
  lastError: null,

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

  updateTask: (id, update) =>
    set((state) => {
      const existingTask = state.tasks[id];

      // If task doesn't exist, create it with the update data
      if (!existingTask) {
        // Only create if we have the required id field
        if (!update.id) return state;

        return {
          tasks: {
            ...state.tasks,
            [id]: update as TaskState,
          },
        };
      }

      return {
        tasks: {
          ...state.tasks,
          [id]: {
            ...existingTask,
            ...update,
          },
        },
      };
    }),

  syncState: (runtimeState) =>
    set({
      agents: runtimeState.agents,
      tasks: runtimeState.tasks,
      stats: runtimeState.stats,
      isPaused: runtimeState.is_paused,
      isPausedByUser: runtimeState.is_paused_by_user,
      isPausedByAgent: runtimeState.is_paused_by_agent,
      pauseState: runtimeState.pause_state,
      currentRunId: runtimeState.current_run_id ?? '',
      orchestratorState: runtimeState.orchestrator_state ?? 'off',
      activeAgentCount: runtimeState.active_agent_count ?? 0,
      // Update event sequence from snapshot - used to discard stale events
      eventSequence: runtimeState.event_sequence ?? 0,
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

  clearOutput: (agentId) =>
    set((state) => {
      const agent = state.agents[agentId];
      if (!agent) return state;

      return {
        agents: {
          ...state.agents,
          [agentId]: {
            ...agent,
            output: {
              stdout: '',
              stderr: '',
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

  appendGitCommit: (agentId, commit) =>
    set((state) => {
      const agent = state.agents[agentId];
      if (!agent) return state;

      const existingCommits = agent.git_commits || [];
      const newCommits = [...existingCommits, commit];

      const newAgents = {
        ...state.agents,
        [agentId]: {
          ...agent,
          git_commits: newCommits,
          commits: newCommits.length,
        },
      };

      return {
        agents: newAgents,
        stats: recalculateStats(newAgents),
      };
    }),

  setSelectedAgent: (selectedAgentId) => set({ selectedAgentId }),

  setHighlightedTask: (highlightedTaskId) => set({ highlightedTaskId }),

  setSelectedBead: (selectedBeadId) => set({ selectedBeadId }),

  setPauseState: (isPaused, isPausedByUser, isPausedByAgent, pauseState) =>
    set({ isPaused, isPausedByUser, isPausedByAgent, pauseState }),

  setRepositories: (repositories, activeRepoId) =>
    set({
      repositories,
      activeRepoId,
    }),

  setActiveRepo: (repoId) =>
    set((state) => ({
      activeRepoId: repoId,
      repositories: state.repositories.map((repo) => ({
        ...repo,
        is_active: repo.id === repoId,
      })),
    })),

  setRepoSwitching: (isRepoSwitching) => set({ isRepoSwitching }),

  setMergeQueue: (mergeQueue) => set({ mergeQueue }),

  updateAgentMergeStatus: (agentId, mergeStatus, queuePos, error, commitsApplied, validationStatus, validationSteps, validationDurationMs, validationError, repairAttempts, lastRepairOutput) =>
    set((state) => {
      const agent = state.agents[agentId];
      if (!agent) {
        // Agent may not exist yet - this can happen if merge status arrives
        // before agent:started. Log and skip silently.
        console.debug('[StateStore] Agent not found for merge status update:', agentId);
        return state;
      }

      // Build update object with proper handling of optional fields
      const updatedAgent: AgentState = {
        ...agent,
        merge_status: mergeStatus,
      };

      // Only set fields if provided (exactOptionalPropertyTypes compliance)
      if (queuePos !== undefined) {
        updatedAgent.merge_queue_pos = queuePos;
      }
      if (error !== undefined) {
        updatedAgent.merge_error = error;
      }
      if (commitsApplied !== undefined) {
        updatedAgent.merge_commits_applied = commitsApplied;
      }
      if (validationStatus !== undefined) {
        updatedAgent.validation_status = validationStatus;
      }
      if (validationSteps !== undefined) {
        updatedAgent.validation_steps = validationSteps;
      }
      if (validationDurationMs !== undefined) {
        updatedAgent.validation_duration_ms = validationDurationMs;
      }
      if (validationError !== undefined) {
        updatedAgent.validation_error = validationError;
      }
      if (repairAttempts !== undefined) {
        updatedAgent.repair_attempts = repairAttempts;
      }
      if (lastRepairOutput !== undefined) {
        updatedAgent.last_repair_output = lastRepairOutput;
      }

      return {
        agents: {
          ...state.agents,
          [agentId]: updatedAgent,
        },
      };
    }),

  // Current run tracking
  setCurrentRunId: (currentRunId) => set({ currentRunId }),

  // Orchestrator state
  setOrchestratorState: (orchestratorState, activeAgentCount) =>
    set({ orchestratorState, activeAgentCount }),

  // Run filtering actions
  setRuns: (runs) => set({ runs }),

  setActiveRunId: (activeRunId) => set({ activeRunId }),

  setRunsLoading: (isRunsLoading) => set({ isRunsLoading }),

  addRun: (run) =>
    set((state) => {
      // Check if run already exists (avoid duplicates)
      if (state.runs.some((r) => r.id === run.id)) {
        return state;
      }
      // Add new run at the beginning (most recent first)
      return { runs: [run, ...state.runs] };
    }),

  updateRun: (runId, update) =>
    set((state) => {
      const runs = state.runs.map((run) =>
        run.id === runId ? { ...run, ...update } : run
      );
      return { runs };
    }),

  // Run control actions
  setStartingRun: (isStartingRun) => set({ isStartingRun }),

  setStoppingRun: (isStoppingRun) => set({ isStoppingRun }),

  setRunConfig: (config) =>
    set((state) => ({
      runConfig: { ...state.runConfig, ...config },
    })),

  setShowRunConfigDialog: (showRunConfigDialog) => set({ showRunConfigDialog }),

  setActiveOrchestratorRun: (activeOrchestratorRun) => set({ activeOrchestratorRun }),

  updateActiveOrchestratorRun: (update) =>
    set((state) => {
      if (!state.activeOrchestratorRun) return state;
      return {
        activeOrchestratorRun: { ...state.activeOrchestratorRun, ...update },
      };
    }),

  // Rules actions - unified format
  setRulesState: (rules, persisted) =>
    set({ rules, rulesPersistedState: persisted }),

  setRulesLoading: (isRulesLoading) => set({ isRulesLoading }),

  setShowAddRuleDialog: (showAddRuleDialog) => set({ showAddRuleDialog }),

  addRule: (rule) =>
    set((state) => ({
      rules: [...state.rules, rule],
    })),

  updateRule: (name, update) =>
    set((state) => {
      const index = state.rules.findIndex((r) => r.name === name);
      if (index === -1) return state;
      const newRules = [...state.rules];
      newRules[index] = { ...newRules[index]!, ...update };
      return { rules: newRules };
    }),

  removeRule: (name) =>
    set((state) => ({
      rules: state.rules.filter((r) => r.name !== name),
    })),

  reorderRules: (fromIndex, toIndex) =>
    set((state) => {
      if (fromIndex === toIndex) return state;
      const newRules = [...state.rules];
      const [removed] = newRules.splice(fromIndex, 1);
      if (removed) {
        newRules.splice(toIndex, 0, removed);
      }
      return {
        rules: newRules,
        rulesPersistedState: false, // Reordering changes persisted state
      };
    }),

  // Optimistic update actions - Start Run
  startOptimisticRun: (tempRunId) =>
    set((state) => {
      const operation: OptimisticOperation = {
        id: `start_run_${Date.now()}`,
        type: 'start_run',
        previousState: {
          orchestratorState: state.orchestratorState,
          currentRunId: state.currentRunId,
          isStartingRun: state.isStartingRun,
        },
        timestamp: Date.now(),
      };
      return {
        pendingOperations: [...state.pendingOperations, operation],
        orchestratorState: 'active' as OrchestratorState,
        currentRunId: tempRunId,
        isStartingRun: true,
        lastError: null,
      };
    }),

  confirmRunStarted: (runId) =>
    set((state) => ({
      pendingOperations: state.pendingOperations.filter((op) => op.type !== 'start_run'),
      currentRunId: runId,
      isStartingRun: false,
    })),

  rollbackRunStart: (error) =>
    set((state) => {
      const operation = state.pendingOperations.find((op) => op.type === 'start_run');
      if (!operation) {
        return { isStartingRun: false, lastError: { operation: 'start_run', message: error } };
      }
      return {
        pendingOperations: state.pendingOperations.filter((op) => op.type !== 'start_run'),
        orchestratorState: operation.previousState.orchestratorState ?? state.orchestratorState,
        currentRunId: operation.previousState.currentRunId ?? '',
        isStartingRun: false,
        lastError: { operation: 'start_run', message: error },
      };
    }),

  // Optimistic update actions - Stop Run
  startOptimisticStop: () =>
    set((state) => {
      const operation: OptimisticOperation = {
        id: `stop_run_${Date.now()}`,
        type: 'stop_run',
        previousState: {
          orchestratorState: state.orchestratorState,
          currentRunId: state.currentRunId,
          isStoppingRun: state.isStoppingRun,
        },
        timestamp: Date.now(),
      };
      return {
        pendingOperations: [...state.pendingOperations, operation],
        orchestratorState: 'idle' as OrchestratorState,
        isStoppingRun: true,
        lastError: null,
      };
    }),

  confirmRunStopped: () =>
    set((state) => ({
      pendingOperations: state.pendingOperations.filter((op) => op.type !== 'stop_run'),
      currentRunId: '',
      isStoppingRun: false,
    })),

  rollbackRunStop: (error) =>
    set((state) => {
      const operation = state.pendingOperations.find((op) => op.type === 'stop_run');
      if (!operation) {
        return { isStoppingRun: false, lastError: { operation: 'stop_run', message: error } };
      }
      return {
        pendingOperations: state.pendingOperations.filter((op) => op.type !== 'stop_run'),
        orchestratorState: operation.previousState.orchestratorState ?? state.orchestratorState,
        currentRunId: operation.previousState.currentRunId ?? state.currentRunId,
        isStoppingRun: false,
        lastError: { operation: 'stop_run', message: error },
      };
    }),

  // Optimistic update actions - Pause
  startOptimisticPause: () =>
    set((state) => {
      const operation: OptimisticOperation = {
        id: `pause_orch_${Date.now()}`,
        type: 'pause_orch',
        previousState: {
          pauseState: state.pauseState,
          isPaused: state.isPaused,
          isPausedByUser: state.isPausedByUser,
          isPausedByAgent: state.isPausedByAgent,
        },
        timestamp: Date.now(),
      };
      // Determine new pause state based on current state
      const newPauseState: PauseState = state.isPausedByAgent ? 'paused_both' : 'paused_user';
      return {
        pendingOperations: [...state.pendingOperations, operation],
        pauseState: newPauseState,
        isPaused: true,
        isPausedByUser: true,
        lastError: null,
      };
    }),

  confirmPause: (pauseState) =>
    set((state) => ({
      pendingOperations: state.pendingOperations.filter((op) => op.type !== 'pause_orch'),
      pauseState,
      isPaused: pauseState !== 'running',
      isPausedByUser: pauseState === 'paused_user' || pauseState === 'paused_both',
      isPausedByAgent: pauseState === 'paused_agent' || pauseState === 'paused_both',
    })),

  rollbackPause: (error) =>
    set((state) => {
      const operation = state.pendingOperations.find((op) => op.type === 'pause_orch');
      if (!operation) {
        return { lastError: { operation: 'pause_orch', message: error } };
      }
      return {
        pendingOperations: state.pendingOperations.filter((op) => op.type !== 'pause_orch'),
        pauseState: operation.previousState.pauseState ?? state.pauseState,
        isPaused: operation.previousState.isPaused ?? state.isPaused,
        isPausedByUser: operation.previousState.isPausedByUser ?? state.isPausedByUser,
        isPausedByAgent: operation.previousState.isPausedByAgent ?? state.isPausedByAgent,
        lastError: { operation: 'pause_orch', message: error },
      };
    }),

  // Optimistic update actions - Resume
  startOptimisticResume: () =>
    set((state) => {
      const operation: OptimisticOperation = {
        id: `resume_orch_${Date.now()}`,
        type: 'resume_orch',
        previousState: {
          pauseState: state.pauseState,
          isPaused: state.isPaused,
          isPausedByUser: state.isPausedByUser,
          isPausedByAgent: state.isPausedByAgent,
        },
        timestamp: Date.now(),
      };
      // When user resumes, only the user pause is cleared
      const newPauseState: PauseState = state.isPausedByAgent ? 'paused_agent' : 'running';
      return {
        pendingOperations: [...state.pendingOperations, operation],
        pauseState: newPauseState,
        isPaused: newPauseState !== 'running',
        isPausedByUser: false,
        lastError: null,
      };
    }),

  confirmResume: (pauseState) =>
    set((state) => ({
      pendingOperations: state.pendingOperations.filter((op) => op.type !== 'resume_orch'),
      pauseState,
      isPaused: pauseState !== 'running',
      isPausedByUser: pauseState === 'paused_user' || pauseState === 'paused_both',
      isPausedByAgent: pauseState === 'paused_agent' || pauseState === 'paused_both',
    })),

  rollbackResume: (error) =>
    set((state) => {
      const operation = state.pendingOperations.find((op) => op.type === 'resume_orch');
      if (!operation) {
        return { lastError: { operation: 'resume_orch', message: error } };
      }
      return {
        pendingOperations: state.pendingOperations.filter((op) => op.type !== 'resume_orch'),
        pauseState: operation.previousState.pauseState ?? state.pauseState,
        isPaused: operation.previousState.isPaused ?? state.isPaused,
        isPausedByUser: operation.previousState.isPausedByUser ?? state.isPausedByUser,
        isPausedByAgent: operation.previousState.isPausedByAgent ?? state.isPausedByAgent,
        lastError: { operation: 'resume_orch', message: error },
      };
    }),

  clearLastError: () => set({ lastError: null }),

  // Config persistence actions
  loadSavedConfig: async () => {
    try {
      const response = await getRunConfig();
      if (!response.error) {
        set({
          runConfig: {
            concurrency: response.concurrency,
            max_priority: response.max_priority,
            use_bwrap: response.use_bwrap,
            max_retries: response.max_retries,
          },
        });
      }
    } catch (error) {
      console.error('Failed to load saved run config:', error);
      // Keep default config on error
    }
  },

  updateRunConfigFromServer: (config) =>
    set({
      runConfig: {
        concurrency: config.concurrency,
        max_priority: config.max_priority,
        use_bwrap: config.use_bwrap,
        max_retries: config.max_retries,
      },
    }),
}));
