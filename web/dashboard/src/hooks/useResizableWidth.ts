import { useState, useEffect, useCallback, useRef } from 'react';

export interface UseResizableWidthOptions {
  /** LocalStorage key for persisting width */
  storageKey: string;
  /** Default width in pixels */
  defaultWidth: number;
  /** Minimum allowed width in pixels */
  minWidth: number;
  /** Maximum width as ratio of viewport (0-1) */
  maxWidthRatio: number;
}

export interface UseResizableWidthResult {
  /** Current pane width in pixels */
  width: number;
  /** Whether the pane is currently being resized */
  isResizing: boolean;
  /** Handler to start resizing (attach to mousedown on resize handle) */
  handleResizeStart: (e: React.MouseEvent) => void;
}

/**
 * Custom hook for managing a horizontally resizable pane.
 *
 * Handles:
 * - Mouse drag events for resizing
 * - Respecting min/max width constraints
 * - Persisting width to localStorage
 */
export function useResizableWidth({
  storageKey,
  defaultWidth,
  minWidth,
  maxWidthRatio,
}: UseResizableWidthOptions): UseResizableWidthResult {
  const [width, setWidth] = useState(() => {
    const saved = localStorage.getItem(storageKey);
    return saved ? parseInt(saved, 10) : defaultWidth;
  });

  const [isResizing, setIsResizing] = useState(false);
  const resizeRef = useRef<{ startX: number; startWidth: number } | null>(null);

  // Persist pane width
  useEffect(() => {
    localStorage.setItem(storageKey, String(width));
  }, [storageKey, width]);

  const handleResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsResizing(true);
      resizeRef.current = {
        startX: e.clientX,
        startWidth: width,
      };
    },
    [width]
  );

  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      if (!resizeRef.current) return;

      const deltaX = e.clientX - resizeRef.current.startX;
      const maxWidth = window.innerWidth * maxWidthRatio;
      const newWidth = Math.min(
        maxWidth,
        Math.max(minWidth, resizeRef.current.startWidth + deltaX)
      );
      setWidth(newWidth);
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
  }, [isResizing, minWidth, maxWidthRatio]);

  return {
    width,
    isResizing,
    handleResizeStart,
  };
}
