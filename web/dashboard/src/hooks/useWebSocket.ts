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
    task_id: string;
    task_title: string;
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

interface AgentLiveFeedEvent {
  type: 'agent:live_feed';
  timestamp: string;
  payload: {
    agent_id: string;
    event_type: 'tool_use' | 'file_change' | 'text' | 'tool_result' | 'error';
    data: Record<string, unknown>;
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
    agent_id?: string;
    priority?: number;
  };
}

interface StatsUpdatedEvent {
  type: 'stats:updated';
  timestamp: string;
  payload: unknown;
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
  task_id: string;
  task_title: string;
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
    total_tokens: number;
    cost_usd: number;
  };
  exit_code: number;
  error: string;
  changes: number;
  commits: number;
  git_commits: BackendGitCommit[];
}

interface BackendRuntimeState {
  agents: Record<string, BackendAgentState>;
  tasks: Record<string, {
    id: string;
    title: string;
    status: string;
    agent_id: string;
    priority: number;
    dependencies: string[];
    archived: boolean;
  }>;
  stats: {
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
    all_git_commits: BackendGitCommit[];
  };
  is_paused: boolean;
  start_time: string;
}

interface StateSyncEvent {
  type: 'state:sync';
  timestamp: string;
  payload: BackendRuntimeState;
}

interface OrchPausedEvent {
  type: 'orch:paused';
  timestamp: string;
  payload: {
    paused: boolean;
  };
}

interface OrchResumedEvent {
  type: 'orch:resumed';
  timestamp: string;
  payload: {
    paused: boolean;
  };
}
type EventType =
  | StateSyncEvent
  | AgentStartedEvent
  | AgentOutputEvent
  | AgentLiveFeedEvent
  | AgentCompletedEvent
  | TaskUpdatedEvent
  | StatsUpdatedEvent
  | OrchPausedEvent
  | OrchResumedEvent;

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
    appendLiveFeedEvent,
    syncState,
    setIsPaused
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
              // Transform backend state to frontend format
              const backendState = message.payload;
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
                };
              }

              syncState({
                agents: transformedAgents,
                tasks: backendState.tasks || {},
                stats: backendState.stats ? {
                  ...backendState.stats,
                  all_git_commits: backendState.stats.all_git_commits || [],
                } : {
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
                  all_git_commits: [],
                },
                is_paused: backendState.is_paused,
                start_time: backendState.start_time,
              });
              console.log('[WebSocket] State synced with', Object.keys(transformedAgents).length, 'agents');
              break;
            }

            case 'agent:started': {
              const { agent_id, task_id, task_title } = message.payload;
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
              });
              break;
            }

            case 'agent:output': {
              const { agent_id, output, is_error } = message.payload;
              appendOutput(agent_id, output, is_error);
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

            case 'agent:completed': {
              const { agent_id, error, exit_code, duration, input_tokens, output_tokens, cost_usd, files_changed, commits_created } = message.payload;
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
                },
              });
              break;
            }

            case 'task:updated': {
              const { id, title, status, agent_id, priority } = message.payload;
              console.log('[WebSocket] Task updated:', id, status);
              updateTask(id, {
                ...(title !== undefined && { title }),
                ...(status !== undefined && { status }),
                ...(agent_id !== undefined && { agent_id }),
                ...(priority !== undefined && { priority }),
              });
              break;
            }

            case 'stats:updated': {
              console.log('[WebSocket] Stats updated:', message.payload);
              break;
            }

            case 'orch:paused': {
              console.log('[WebSocket] Orchestrator paused');
              setIsPaused(true);
              break;
            }

            case 'orch:resumed': {
              console.log('[WebSocket] Orchestrator resumed');
              setIsPaused(false);
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
  }, [setConnected, updateAgent, updateTask, appendOutput, appendLiveFeedEvent, syncState, setIsPaused]);

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

  // Get connected state from store
  const connected = useStateStore((state) => state.connected);

  return {
    connected,
    reconnect,
    disconnect,
  };
}
