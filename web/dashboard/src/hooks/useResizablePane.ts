import { useState, useEffect, useCallback, useRef } from 'react';

export interface UseResizablePaneOptions {
  /** LocalStorage key for persisting height */
  storageKey: string;
  /** Default height in pixels */
  defaultHeight: number;
  /** Minimum allowed height in pixels */
  minHeight: number;
  /** Maximum height as ratio of viewport (0-1) */
  maxHeightRatio: number;
}

export interface UseResizablePaneResult {
  /** Current pane height in pixels */
  height: number;
  /** Whether the pane is currently being resized */
  isResizing: boolean;
  /** Handler to start resizing (attach to mousedown on resize handle) */
  handleResizeStart: (e: React.MouseEvent) => void;
}

/**
 * Custom hook for managing a resizable pane.
 *
 * Handles:
 * - Mouse drag events for resizing
 * - Respecting min/max height constraints
 * - Persisting height to localStorage
 */
export function useResizablePane({
  storageKey,
  defaultHeight,
  minHeight,
  maxHeightRatio,
}: UseResizablePaneOptions): UseResizablePaneResult {
  const [height, setHeight] = useState(() => {
    const saved = localStorage.getItem(storageKey);
    return saved ? parseInt(saved, 10) : defaultHeight;
  });

  const [isResizing, setIsResizing] = useState(false);
  const resizeRef = useRef<{ startY: number; startHeight: number } | null>(null);

  // Persist pane height
  useEffect(() => {
    localStorage.setItem(storageKey, String(height));
  }, [storageKey, height]);

  const handleResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsResizing(true);
      resizeRef.current = {
        startY: e.clientY,
        startHeight: height,
      };
    },
    [height]
  );

  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      if (!resizeRef.current) return;

      const deltaY = resizeRef.current.startY - e.clientY;
      const maxHeight = window.innerHeight * maxHeightRatio;
      const newHeight = Math.min(
        maxHeight,
        Math.max(minHeight, resizeRef.current.startHeight + deltaY)
      );
      setHeight(newHeight);
    };

    const handleMouseUp = () => {
      setIsResizing(false);
      resizeRef.current = null;
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isResizing, minHeight, maxHeightRatio]);

  return {
    height,
    isResizing,
    handleResizeStart,
  };
}
