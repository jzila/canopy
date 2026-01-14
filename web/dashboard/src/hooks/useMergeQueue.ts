import { useEffect, useCallback, useRef } from 'react';
import { useStateStore } from '../stores/stateStore';
import { getMergeQueue } from '../api/client';

const POLL_INTERVAL = 2000; // Poll every 2 seconds

export function useMergeQueue() {
  const connected = useStateStore((state) => state.connected);
  const setMergeQueue = useStateStore((state) => state.setMergeQueue);
  const mergeQueue = useStateStore((state) => state.mergeQueue);
  const intervalRef = useRef<number | null>(null);

  const fetchMergeQueue = useCallback(async () => {
    if (!connected) return;

    try {
      const queue = await getMergeQueue();
      setMergeQueue(queue);
    } catch (error) {
      console.error('[MergeQueue] Failed to fetch merge queue:', error);
    }
  }, [connected, setMergeQueue]);

  // Start polling when connected
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

    // Set up polling
    intervalRef.current = window.setInterval(fetchMergeQueue, POLL_INTERVAL);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [connected, fetchMergeQueue]);

  return { mergeQueue, refetch: fetchMergeQueue };
}
