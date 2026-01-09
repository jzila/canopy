import { useEffect, useRef, useCallback } from 'react';
import { useStateStore } from '../stores/stateStore';

// Event types that match the Go backend
interface WebSocketEvent {
  type: string;
  data: unknown;
}

interface AgentStartedEvent {
  type: 'agent:started';
  data: {
    id: string;
    task_id: string;
    task_title: string;
    status: string;
    start_time: string;
  };
}

interface AgentOutputEvent {
  type: 'agent:output';
  data: {
    agent_id: string;
    output: string;
  };
}

interface AgentCompletedEvent {
  type: 'agent:completed';
  data: {
    id: string;
    status: string;
    end_time: string;
    duration: number;
    exit_code: number;
    error: string;
    changes: number;
    commits: number;
  };
}

interface TaskUpdatedEvent {
  type: 'task:updated';
  data: {
    id: string;
    title?: string;
    status?: string;
    agent_id?: string;
    priority?: number;
  };
}

interface StatsUpdatedEvent {
  type: 'stats:updated';
  data: unknown;
}

type EventType =
  | AgentStartedEvent
  | AgentOutputEvent
  | AgentCompletedEvent
  | TaskUpdatedEvent
  | StatsUpdatedEvent;

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
    appendOutput,
    syncState
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

          switch (message.type) {
            case 'agent:started': {
              const { id, task_id, task_title, status, start_time } = message.data;
              updateAgent(id, {
                id,
                task_id,
                task_title,
                status: status as any,
                start_time,
                end_time: null,
                duration: 0,
                output: { stdout: '', stderr: '' },
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
              });
              break;
            }

            case 'agent:output': {
              const { agent_id, output } = message.data;
              appendOutput(agent_id, output);
              break;
            }

            case 'agent:completed': {
              const { id, status, end_time, duration, exit_code, error, changes, commits } = message.data;
              updateAgent(id, {
                status: status as any,
                end_time,
                duration,
                exit_code,
                error,
                changes,
                commits,
              });
              break;
            }

            case 'task:updated': {
              // For now, we could do a partial update or just sync full state
              // The backend might send full state updates, so syncState would handle it
              console.log('[WebSocket] Task updated:', message.data);
              break;
            }

            case 'stats:updated': {
              // Trigger a full state sync since stats affect everything
              console.log('[WebSocket] Stats updated:', message.data);
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
  }, [setConnected, updateAgent, appendOutput, syncState]);

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
