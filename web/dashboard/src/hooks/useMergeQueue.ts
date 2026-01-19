import { useEffect, useCallback, useRef, useMemo } from 'react';
import { useStateStore, type MergeStatus } from '../stores/stateStore';
import { getMergeQueue } from '../api/client';

// Polling interval for fallback sync (longer since we have real-time updates)
const POLL_INTERVAL = 5000; // 5 seconds (increased from 2s since we have WebSocket updates)

// Debounce delay for rapid merge status updates to prevent UI flickering
const DEBOUNCE_DELAY = 150; // 150ms debounce

// Hook for debouncing a callback
function useDebouncedCallback<T extends (...args: unknown[]) => void>(
  callback: T,
  delay: number
): T {
  const timeoutRef = useRef<number | null>(null);
  const callbackRef = useRef(callback);

  // Keep callback ref updated
  useEffect(() => {
    callbackRef.current = callback;
  }, [callback]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useCallback(
    ((...args: unknown[]) => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current);
      }
      timeoutRef.current = window.setTimeout(() => {
        callbackRef.current(...args);
        timeoutRef.current = null;
      }, delay);
    }) as T,
    [delay]
  );
}

export function useMergeQueue() {
  const connected = useStateStore((state) => state.connected);
  const setMergeQueue = useStateStore((state) => state.setMergeQueue);
  const mergeQueue = useStateStore((state) => state.mergeQueue);
  const agents = useStateStore((state) => state.agents);
  const isPaused = useStateStore((state) => state.isPaused);
  const intervalRef = useRef<number | null>(null);

  // Track merge-related state changes to detect when to refetch
  // This creates a fingerprint of merge-related agent states
  const mergeStateFingerprint = useMemo(() => {
    const fingerprint: string[] = [];
    for (const agent of Object.values(agents)) {
      if (agent.merge_status) {
        fingerprint.push(`${agent.id}:${agent.merge_status}:${agent.merge_queue_pos ?? ''}`);
      }
    }
    return fingerprint.sort().join('|');
  }, [agents]);

  const fetchMergeQueue = useCallback(async () => {
    if (!connected) return;

    try {
      const queue = await getMergeQueue();
      setMergeQueue(queue);
    } catch (error) {
      console.error('[MergeQueue] Failed to fetch merge queue:', error);
    }
  }, [connected, setMergeQueue]);

  // Debounced version of fetchMergeQueue to prevent rapid API calls
  const debouncedFetchMergeQueue = useDebouncedCallback(fetchMergeQueue, DEBOUNCE_DELAY);

  // Refetch when merge state fingerprint changes (debounced to prevent flickering)
  // This triggers when WebSocket events update agent merge statuses
  useEffect(() => {
    if (!connected || !mergeStateFingerprint) return;

    // Skip initial empty fingerprint
    if (mergeStateFingerprint === '') return;

    console.debug('[MergeQueue] Merge state changed, triggering debounced refetch');
    debouncedFetchMergeQueue();
  }, [connected, mergeStateFingerprint, debouncedFetchMergeQueue]);

  // Set up polling as fallback for missed events and initial sync
  useEffect(() => {
    if (!connected) {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    // Initial fetch
    fetchMergeQueue();

    // Set up polling (longer interval since we have WebSocket updates)
    intervalRef.current = window.setInterval(fetchMergeQueue, POLL_INTERVAL);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [connected, fetchMergeQueue]);

  // Also refetch when pause state changes
  useEffect(() => {
    if (connected) {
      debouncedFetchMergeQueue();
    }
  }, [connected, isPaused, debouncedFetchMergeQueue]);

  return { mergeQueue, refetch: fetchMergeQueue };
}

// Export MergeStatus type for external use
export type { MergeStatus };
