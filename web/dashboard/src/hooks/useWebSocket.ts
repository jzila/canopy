import { useEffect, useRef, useCallback } from 'react';
import { useStateStore } from '../stores/stateStore';
// Event types that match the Go backend (wire_events.go)
// Backend sends: { type, timestamp, payload }
interface WebSocketEvent {
  type: string;
  timestamp: string;
  payload: unknown;
}
interface AgentStartedEvent {
  type: 'agent:started';
  timestamp: string;
  payload: {
    agent_id: string;
    run_id?: string;
    task_id: string;
    task_title: string;
    task_description?: string;
    parent_agent_id?: string;
  };
}
interface AgentOutputEvent {
  type: 'agent:output';
  timestamp: string;
  payload: {
    agent_id: string;
    output: string;
    is_error: boolean;
  };
}
interface AgentOutputClearEvent {
  type: 'agent:output_clear';
  timestamp: string;
  payload: {
    agent_id: string;
  };
}
interface AgentLiveFeedEvent {
  type: 'agent:live_feed';
  timestamp: string;
  payload: {
    agent_id: string;
    event_type: 'tool_use' | 'file_change' | 'text' | 'tool_result' | 'error';
    data: Record<string, unknown>;
  };
}
interface AgentCommitEvent {
  type: 'agent:commit';
  timestamp: string;
  payload: {
    agent_id: string;
    hash: string;
    short_hash: string;
    message: string;
    author: string;
    author_email: string;
    timestamp: string;
    files_changed: string[];
  };
}
interface AgentCompletedEvent {
  type: 'agent:completed';
  timestamp: string;
  payload: {
    agent_id: string;
    error?: string;
    exit_code: number;
    duration: number;
    input_tokens: number;
    output_tokens: number;
    cache_creation_input_tokens?: number;
    cache_read_input_tokens?: number;
    cost_usd: number;
    files_changed: number;
    commits_created: number;
  };
}
interface TaskUpdatedEvent {
  type: 'task:updated';
  timestamp: string;
  payload: {
    id: string;
    title?: string;
    status?: string;
    type?: string;     // Task type (task, bug, feature, etc.)
    priority?: number;
    agent_id?: string;
    updated_at?: number;  // Unix timestamp of last update
  };
}
// Merge status types matching Go backend (ipc/protocol.go)
type MergeStatus = 'pending' | 'acquiring' | 'merging' | 'resolving' | 'merged' | 'failed' | 'skipped' | 'merged_needs_repair';
// Validation status types matching Go backend (validation/executor.go)
type ValidationStatus = 'pending' | 'running' | 'passed' | 'failed' | 'skipped' | 'repairing';
// Validation step result matching Go backend (ipc/protocol.go)
interface ValidationStep {
  name: string;
  status: string;
  duration_ms: number;
  output?: string;
}
interface AgentMergeStatusEvent {
  type: 'agent:merge_status';
  timestamp: string;
  payload: {
    agent_id: string;
    merge_status: MergeStatus;
    queue_pos?: number;
    error?: string;
    // Validation results
    validation_status?: ValidationStatus;
    validation_steps?: ValidationStep[];
    validation_duration_ms?: number;
    validation_error?: string;
    // Repair agent tracking
    repair_attempts?: number;
    last_repair_output?: string;
  };
}
interface StatsUpdatedEvent {
  type: 'stats:updated';
  timestamp: string;
  payload: unknown;
}
interface RunStartedEvent {
  type: 'run:started';
  timestamp: string;
  payload: {
    run_id: string;
    task_count: number;
    repo_id?: string;
    repo_path?: string;
    repo_name?: string;
  };
}
interface RunCompletedEvent {
  type: 'run:completed';
  timestamp: string;
  payload: {
    run_id: string;
    total_tasks: number;
    succeeded_tasks: number;
    failed_tasks: number;
    total_duration_seconds: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_cache_creation_input_tokens: number;
    total_cache_read_input_tokens: number;
    total_cost_usd: number;
    total_turns: number;
    files_changed: number;
    git_commits: number;
    conflicts_resolved: number;
  };
}
// Rules configuration types - unified format
interface Rule {
  name: string;
  conditions: string[];
  action: 'deny' | 'allow';
  enabled: boolean;
  persisted: boolean;
}
interface RulesChangedEvent {
  type: 'rules:changed';
  timestamp: string;
  payload: {
    action: 'added' | 'updated' | 'deleted';
    rule?: Rule;
  };
}
// Backend git commit format
interface BackendGitCommit {
  hash: string;
  short_hash: string;
  message: string;
  author: string;
  author_email: string;
  timestamp: string;
  files_changed: string[];
}
// Backend RuntimeState format (snake_case)
interface BackendAgentState {
  id: string;
  run_id?: string;
  task_id: string;
  task_title: string;
  task_description?: string;
  status: string;
  start_time: string;
  end_time: string | null;
  duration: number;
  output: { stdout: string; stderr: string };
  live_feed_events: Array<{
    event_type: string;
    data: Record<string, unknown>;
  }>;
  token_usage: {
    input_tokens: number;
    output_tokens: number;
    cache_creation_input_tokens?: number;
    cache_read_input_tokens?: number;
    total_tokens: number;
    cost_usd: number;
  };
  exit_code: number;
  error: string;
  changes: number;
  commits: number;
  git_commits: BackendGitCommit[];
  archived: boolean;
  parent_agent_id?: string;
  // Merge status fields
  merge_status?: string;
  merge_queue_pos?: number;
  merge_error?: string;
  // Validation and repair fields
  validation_status?: string;
  validation_steps?: Array<{
    name: string;
    status: string;
    duration_ms: number;
    output?: string;
  }>;
  validation_duration_ms?: number;
  validation_error?: string;
  repair_attempts?: number;
  last_repair_output?: string;
}
// Backend task state format
interface BackendTaskState {
  id: string;
  title: string;
  status: string;
  type?: string;     // Task type (task, bug, feature, etc.)
  agent_id: string;
  priority: number;
  dependencies: string[];
  archived: boolean;
  repo_id?: string;
  updated_at?: number;  // Unix timestamp of last update
}
interface BackendRuntimeState {
  agents: Record<string, BackendAgentState>;
  tasks: Record<string, BackendTaskState>;  // Legacy: merged view for backwards compat
  // Hybrid overlay architecture: dual-source task state
  persistent_tasks?: Record<string, BackendTaskState>;  // From beads (source of truth)
  runtime_tasks?: Record<string, BackendTaskState>;     // Ephemeral overlay (in_progress, agent assignments)
  stats: {
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
    all_git_commits: BackendGitCommit[];
  };
  is_paused: boolean;
  is_paused_by_user: boolean;
  is_paused_by_agent: boolean;
  pause_state: PauseState;
  start_time: string;
  current_run_id: string;
}
// Pause state enum matching Go backend (ipc/protocol.go)
type PauseState = 'running' | 'paused_user' | 'paused_agent' | 'paused_both';
interface StateSyncEvent {
  type: 'state:sync';
  timestamp: string;
  payload: BackendRuntimeState | RepoSyncPayload;
}
// Payload when switching repos (partial sync)
interface RepoSyncPayload {
  active_repo_id: string;
  active_repo_name: string;
  active_repo_path: string;
}
interface OrchPauseStatusPayload {
  is_paused: boolean;
  is_paused_by_user: boolean;
  is_paused_by_agent: boolean;
  pause_state: PauseState;
}
interface OrchPausedEvent {
  type: 'orch:paused';
  timestamp: string;
  payload: OrchPauseStatusPayload;
}
interface OrchResumedEvent {
  type: 'orch:resumed';
  timestamp: string;
  payload: OrchPauseStatusPayload;
}
type EventType =
  | StateSyncEvent
  | AgentStartedEvent
  | AgentOutputEvent
  | AgentOutputClearEvent
  | AgentLiveFeedEvent
  | AgentCommitEvent
  | AgentCompletedEvent
  | AgentMergeStatusEvent
  | TaskUpdatedEvent
  | StatsUpdatedEvent
  | OrchPausedEvent
  | OrchResumedEvent
  | RunStartedEvent
  | RunCompletedEvent
  | RulesChangedEvent;
const MAX_BACKOFF = 30000; // 30 seconds
const INITIAL_BACKOFF = 1000; // 1 second
function getWebSocketURL(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  // In development, use the Vite proxy which forwards to the backend
  if (import.meta.env.DEV) {
    return `${protocol}//${window.location.host}/ws`;
  }
  // In production, connect to the same host
  return `${protocol}//${window.location.host}/ws`;
}
export function useWebSocket() {
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const backoffTimeRef = useRef<number>(INITIAL_BACKOFF);
  const isManuallyClosedRef = useRef<boolean>(false);
  const {
    setConnected,
    updateAgent,
    updateTask,
    appendOutput,
    clearOutput,
    appendLiveFeedEvent,
    appendGitCommit,
    syncState,
    setPauseState,
    setActiveRepo,
    updateAgentMergeStatus,
    addRun,
    updateRun,
    setCurrentRunId,
  } = useStateStore();
  const connect = useCallback(() => {
    // Don't reconnect if manually closed
    if (isManuallyClosedRef.current) {
      return;
    }
    // Clean up existing connection
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    const wsURL = getWebSocketURL();
    console.log('[WebSocket] Connecting to', wsURL);
    try {
      const ws = new WebSocket(wsURL);
      wsRef.current = ws;
      ws.onopen = () => {
        console.log('[WebSocket] Connected');
        setConnected(true);
        backoffTimeRef.current = INITIAL_BACKOFF; // Reset backoff on successful connection
      };
      ws.onmessage = (event) => {
        try {
          const message: EventType = JSON.parse(event.data);
          console.log('[WebSocket] Received:', message.type, message);
          switch (message.type) {
            case 'state:sync': {
              // Check if this is a repo switch event (partial sync)
              const payload = message.payload as unknown as Record<string, unknown>;
              if ('active_repo_id' in payload && !('agents' in payload)) {
                // This is a repo switch notification
                const repoPayload = payload as unknown as RepoSyncPayload;
                console.log('[WebSocket] Repo switched to:', repoPayload.active_repo_id);
                setActiveRepo(repoPayload.active_repo_id);
                break;
              }
              // Transform backend state to frontend format (full sync)
              const backendState = message.payload as unknown as BackendRuntimeState;
              const transformedAgents: Record<string, import('../stores/stateStore').AgentState> = {};
              for (const [id, agent] of Object.entries(backendState.agents)) {
                // Transform live_feed_events to liveFeed with generated IDs
                const liveFeed = (agent.live_feed_events || []).map((event, index) => ({
                  id: `${id}-sync-${index}`,
                  timestamp: backendState.start_time, // Use start_time as fallback
                  event_type: event.event_type as 'tool_use' | 'file_change' | 'text' | 'tool_result' | 'error',
                  data: event.data || {},
                }));
                transformedAgents[id] = {
                  id: agent.id,
                  task_id: agent.task_id,
                  task_title: agent.task_title,
                  status: agent.status as import('../stores/stateStore').AgentStatus,
                  start_time: agent.start_time,
                  end_time: agent.end_time,
                  duration: agent.duration,
                  output: agent.output || { stdout: '', stderr: '' },
                  liveFeed,
                  token_usage: agent.token_usage || {
                    input_tokens: 0,
                    output_tokens: 0,
                    total_tokens: 0,
                    cost_usd: 0,
                  },
                  exit_code: agent.exit_code,
                  error: agent.error || '',
                  changes: agent.changes,
                  commits: agent.commits,
                  git_commits: agent.git_commits || [],
                  archived: agent.archived || false,
                  // Optional properties - only set if defined (exactOptionalPropertyTypes compliance)
                  ...(agent.run_id && { run_id: agent.run_id }),
                  ...(agent.task_description && { task_description: agent.task_description }),
                  ...(agent.parent_agent_id && { parent_agent_id: agent.parent_agent_id }),
                  // Merge status fields
                  ...(agent.merge_status && { merge_status: agent.merge_status as import('../stores/stateStore').MergeStatus }),
                  ...(agent.merge_queue_pos !== undefined && { merge_queue_pos: agent.merge_queue_pos }),
                  ...(agent.merge_error && { merge_error: agent.merge_error }),
                  // Validation and repair fields
                  ...(agent.validation_status && { validation_status: agent.validation_status as import('../stores/stateStore').ValidationStatus }),
                  ...(agent.validation_steps && { validation_steps: agent.validation_steps }),
                  ...(agent.validation_duration_ms !== undefined && { validation_duration_ms: agent.validation_duration_ms }),
                  ...(agent.validation_error && { validation_error: agent.validation_error }),
                  ...(agent.repair_attempts !== undefined && { repair_attempts: agent.repair_attempts }),
                  ...(agent.last_repair_output && { last_repair_output: agent.last_repair_output }),
                };
              }
              // Merge dual-source tasks: runtime overlays persistent for display
              // If new dual-source fields exist, merge them; otherwise fall back to legacy tasks
              let mergedTasks: Record<string, import('../stores/stateStore').TaskState> = {};
              if (backendState.persistent_tasks && Object.keys(backendState.persistent_tasks).length > 0) {
                // Use new hybrid overlay architecture
                const persistentTasks = backendState.persistent_tasks;
                const runtimeTasks = backendState.runtime_tasks || {};
                // Start with persistent tasks (base layer from beads)
                for (const [id, task] of Object.entries(persistentTasks)) {
                  mergedTasks[id] = {
                    id: task.id,
                    title: task.title,
                    status: task.status,
                    agent_id: task.agent_id || '',
                    priority: task.priority,
                    dependencies: task.dependencies || [],
                    archived: task.archived || false,
                    ...(task.type && { type: task.type }),
                    ...(task.updated_at !== undefined && { updated_at: task.updated_at }),
                  };
                }
                // Overlay runtime state (in_progress, agent assignments)
                for (const [id, rt] of Object.entries(runtimeTasks)) {
                  if (mergedTasks[id]) {
                    // Merge runtime onto persistent
                    if (rt.status) mergedTasks[id].status = rt.status;
                    if (rt.agent_id) mergedTasks[id].agent_id = rt.agent_id;
                  } else {
                    // Runtime-only task (shouldn't happen often, but handle gracefully)
                    mergedTasks[id] = {
                      id: rt.id,
                      title: rt.title || '',
                      status: rt.status || 'unknown',
                      agent_id: rt.agent_id || '',
                      priority: rt.priority || 0,
                      dependencies: rt.dependencies || [],
                      archived: rt.archived || false,
                    };
                  }
                }
                console.log('[WebSocket] Merged dual-source tasks:', Object.keys(persistentTasks).length, 'persistent,', Object.keys(runtimeTasks).length, 'runtime');
              } else {
                // Fall back to legacy tasks field for backwards compatibility
                mergedTasks = (backendState.tasks || {}) as Record<string, import('../stores/stateStore').TaskState>;
              }
              syncState({
                agents: transformedAgents,
                tasks: mergedTasks,
                stats: backendState.stats ? {
                  ...backendState.stats,
                  all_git_commits: backendState.stats.all_git_commits || [],
                } : {
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
                },
                is_paused: backendState.is_paused,
                is_paused_by_user: backendState.is_paused_by_user ?? false,
                is_paused_by_agent: backendState.is_paused_by_agent ?? false,
                pause_state: backendState.pause_state ?? 'running',
                start_time: backendState.start_time,
                current_run_id: backendState.current_run_id ?? '',
              });
              console.log('[WebSocket] State synced with', Object.keys(transformedAgents).length, 'agents');
              break;
            }
            case 'agent:started': {
              const { agent_id, run_id, task_id, task_title, task_description, parent_agent_id } = message.payload;
              // Create new agent entry
              updateAgent(agent_id, {
                id: agent_id,
                task_id,
                task_title,
                status: 'running',
                start_time: message.timestamp,
                end_time: null,
                duration: 0,
                output: { stdout: '', stderr: '' },
                liveFeed: [],
                token_usage: {
                  input_tokens: 0,
                  output_tokens: 0,
                  total_tokens: 0,
                  cost_usd: 0,
                },
                exit_code: 0,
                error: '',
                changes: 0,
                commits: 0,
                git_commits: [],
                // Optional properties - only set if defined (exactOptionalPropertyTypes compliance)
                ...(run_id && { run_id }),
                ...(task_description && { task_description }),
                ...(parent_agent_id && { parent_agent_id }),
              });
              break;
            }
            case 'agent:output': {
              const { agent_id, output, is_error } = message.payload;
              appendOutput(agent_id, output, is_error);
              break;
            }
            case 'agent:output_clear': {
              const { agent_id } = message.payload;
              clearOutput(agent_id);
              break;
            }
            case 'agent:live_feed': {
              const { agent_id, event_type, data } = message.payload;
              // Generate unique ID for the event
              const eventId = `${agent_id}-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
              appendLiveFeedEvent(agent_id, {
                id: eventId,
                timestamp: message.timestamp,
                event_type,
                data,
              });
              break;
            }
            case 'agent:commit': {
              const { agent_id, hash, short_hash, message: commitMessage, author, author_email, timestamp, files_changed } = message.payload;
              console.log('[WebSocket] Agent commit:', agent_id, short_hash, commitMessage);
              appendGitCommit(agent_id, {
                hash,
                short_hash,
                message: commitMessage,
                author,
                author_email,
                timestamp,
                files_changed: files_changed || [],
              });
              break;
            }
            case 'agent:completed': {
              const { agent_id, error, exit_code, duration, input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens, cost_usd, files_changed, commits_created } = message.payload;
              updateAgent(agent_id, {
                status: error ? 'failed' : 'completed',
                end_time: message.timestamp,
                duration,
                exit_code,
                error: error || '',
                changes: files_changed,
                commits: commits_created,
                token_usage: {
                  input_tokens,
                  output_tokens,
                  total_tokens: input_tokens + output_tokens,
                  cost_usd,
                  // Optional cache token fields - only set if defined (exactOptionalPropertyTypes compliance)
                  ...(cache_creation_input_tokens !== undefined && { cache_creation_input_tokens }),
                  ...(cache_read_input_tokens !== undefined && { cache_read_input_tokens }),
                },
              });
              break;
            }
            case 'agent:merge_status': {
              const {
                agent_id,
                merge_status,
                queue_pos,
                error,
                validation_status,
                validation_steps,
                validation_duration_ms,
                validation_error,
                repair_attempts,
                last_repair_output,
              } = message.payload;
              console.log('[WebSocket] Agent merge status:', agent_id, merge_status, 'pos:', queue_pos, 'validation:', validation_status);
              updateAgentMergeStatus(
                agent_id,
                merge_status,
                queue_pos,
                error,
                validation_status as import('../stores/stateStore').ValidationStatus | undefined,
                validation_steps as import('../stores/stateStore').ValidationStep[] | undefined,
                validation_duration_ms,
                validation_error,
                repair_attempts,
                last_repair_output
              );
              break;
            }
            case 'task:updated': {
              const { id, title, status, type: taskType, priority, agent_id, updated_at } = message.payload;
              console.log('[WebSocket] Task updated:', id, status);
              updateTask(id, {
                ...(title !== undefined && { title }),
                ...(status !== undefined && { status }),
                ...(taskType !== undefined && { type: taskType }),
                ...(priority !== undefined && { priority }),
                ...(agent_id !== undefined && { agent_id }),
                ...(updated_at !== undefined && { updated_at }),
              });
              break;
            }
            case 'stats:updated': {
              console.log('[WebSocket] Stats updated:', message.payload);
              break;
            }
            case 'orch:paused': {
              const { is_paused, is_paused_by_user, is_paused_by_agent, pause_state } = message.payload;
              console.log('[WebSocket] Orchestrator paused:', pause_state);
              setPauseState(is_paused, is_paused_by_user, is_paused_by_agent, pause_state);
              break;
            }
            case 'orch:resumed': {
              const { is_paused, is_paused_by_user, is_paused_by_agent, pause_state } = message.payload;
              console.log('[WebSocket] Orchestrator resumed:', pause_state);
              setPauseState(is_paused, is_paused_by_user, is_paused_by_agent, pause_state);
              break;
            }
            case 'run:started': {
              const { run_id, task_count, repo_id, repo_path, repo_name } = message.payload;
              console.log('[WebSocket] Run started:', run_id, 'tasks:', task_count);
              // Set the current run ID for the orchestrator
              setCurrentRunId(run_id);
              addRun({
                id: run_id,
                started_at: message.timestamp,
                status: 'running',
                total_tasks: task_count,
                completed_tasks: 0,
                failed_tasks: 0,
                total_cost_usd: 0,
                duration_seconds: 0,
                // Optional properties - only set if defined
                ...(repo_id && { repo_id }),
                ...(repo_path && { repo_path }),
                ...(repo_name && { repo_name }),
              });
              break;
            }
            case 'run:completed': {
              const {
                run_id,
                total_tasks,
                succeeded_tasks,
                failed_tasks,
                total_duration_seconds,
                total_cost_usd,
              } = message.payload;
              console.log('[WebSocket] Run completed:', run_id, 'succeeded:', succeeded_tasks, 'failed:', failed_tasks);
              // Clear the current run ID as the run has completed
              setCurrentRunId('');
              // Determine status based on results
              const status = failed_tasks > 0 ? 'partial' : 'completed';
              updateRun(run_id, {
                finished_at: message.timestamp,
                status,
                total_tasks,
                completed_tasks: succeeded_tasks,
                failed_tasks,
                total_cost_usd,
                duration_seconds: total_duration_seconds,
              });
              break;
            }
            case 'rules:changed': {
              const { action, rule } = message.payload;
              console.log('[WebSocket] Rules changed:', action, rule?.name);
              // Rules changes are informational for now - UI can fetch updated rules if needed
              break;
            }
            default:
              console.warn('[WebSocket] Unknown event type:', (message as WebSocketEvent).type);
          }
        } catch (err) {
          console.error('[WebSocket] Failed to parse message:', err, event.data);
        }
      };
      ws.onerror = (error) => {
        console.error('[WebSocket] Error:', error);
      };
      ws.onclose = (event) => {
        console.log('[WebSocket] Disconnected', event.code, event.reason);
        setConnected(false);
        wsRef.current = null;
        // Attempt reconnect with exponential backoff
        if (!isManuallyClosedRef.current) {
          const backoffTime = backoffTimeRef.current;
          console.log(`[WebSocket] Reconnecting in ${backoffTime}ms...`);
          reconnectTimeoutRef.current = window.setTimeout(() => {
            // Increase backoff time for next attempt, up to max
            backoffTimeRef.current = Math.min(backoffTime * 2, MAX_BACKOFF);
            connect();
          }, backoffTime);
        }
      };
    } catch (err) {
      console.error('[WebSocket] Failed to create WebSocket:', err);
      setConnected(false);
      // Retry connection
      if (!isManuallyClosedRef.current) {
        const backoffTime = backoffTimeRef.current;
        reconnectTimeoutRef.current = window.setTimeout(() => {
          backoffTimeRef.current = Math.min(backoffTime * 2, MAX_BACKOFF);
          connect();
        }, backoffTime);
      }
    }
  }, [setConnected, updateAgent, updateTask, appendOutput, appendLiveFeedEvent, appendGitCommit, syncState, setPauseState, setActiveRepo, updateAgentMergeStatus, addRun, updateRun, clearOutput, setCurrentRunId]);
  const disconnect = useCallback(() => {
    isManuallyClosedRef.current = true;
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setConnected(false);
  }, [setConnected]);
  const reconnect = useCallback(() => {
    isManuallyClosedRef.current = false;
    backoffTimeRef.current = INITIAL_BACKOFF;
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    connect();
  }, [connect]);
  // Auto-connect on mount
  useEffect(() => {
    isManuallyClosedRef.current = false;
    connect();
    // Cleanup on unmount
    return () => {
      isManuallyClosedRef.current = true;
      if (reconnectTimeoutRef.current !== null) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [connect]);
  return { connect, disconnect, reconnect };
}
