import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, Clock, Loader2, Check, List } from 'lucide-react';
import type { Run } from '../../api/client';

interface RunSelectorProps {
  runs: Run[];
  activeRunId: string; // empty string means "All runs"
  onSelect: (runId: string) => void;
  isLoading?: boolean;
  disabled?: boolean;
}

export const RunSelector: React.FC<RunSelectorProps> = ({
  runs,
  activeRunId,
  onSelect,
  isLoading = false,
  disabled = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const activeRun = runs.find((run) => run.id === activeRunId);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Close on escape
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleEscape);
      return () => document.removeEventListener('keydown', handleEscape);
    }
  }, [isOpen]);

  const handleSelect = (runId: string) => {
    if (runId !== activeRunId) {
      onSelect(runId);
    }
    setIsOpen(false);
  };

  const formatRunId = (id: string): string => {
    // Show first 8 characters of run ID
    return id.length > 8 ? id.slice(0, 8) : id;
  };

  const formatDate = (dateStr: string): string => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return 'just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;
    return date.toLocaleDateString();
  };

  const getStatusColor = (status: Run['status']): string => {
    switch (status) {
      case 'running':
        return 'bg-blue-500';
      case 'completed':
        return 'bg-green-500';
      case 'failed':
        return 'bg-red-500';
      case 'partial':
        return 'bg-amber-500';
      case 'cancelled':
        return 'bg-gray-500';
      default:
        return 'bg-gray-400';
    }
  };

  // If there are no runs, show disabled state
  if (runs.length === 0) {
    return (
      <div className="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-700 rounded-lg text-gray-500 dark:text-gray-400">
        <Clock className="w-4 h-4" />
        <span className="text-sm">No runs</span>
      </div>
    );
  }

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => !disabled && !isLoading && setIsOpen(!isOpen)}
        disabled={disabled || isLoading}
        className={`
          flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-700 rounded-lg
          transition-colors min-w-[160px] max-w-[220px]
          ${disabled || isLoading
            ? 'opacity-50 cursor-not-allowed'
            : 'hover:bg-gray-200 dark:hover:bg-gray-600 cursor-pointer'
          }
        `}
        title={activeRun ? `Run: ${activeRun.id}` : 'All runs'}
      >
        {isLoading ? (
          <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
        ) : activeRunId === '' ? (
          <List className="w-4 h-4 text-gray-600 dark:text-gray-400 flex-shrink-0" />
        ) : (
          <Clock className="w-4 h-4 text-gray-600 dark:text-gray-400 flex-shrink-0" />
        )}
        <div className="flex flex-col items-start min-w-0 flex-1">
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate w-full text-left">
            {activeRunId === '' ? 'All runs' : `Run ${formatRunId(activeRunId)}`}
          </span>
          {activeRun && (
            <span className="text-xs text-gray-500 dark:text-gray-400 truncate w-full text-left">
              {formatDate(activeRun.started_at)}
            </span>
          )}
        </div>
        <ChevronDown
          className={`w-4 h-4 text-gray-500 dark:text-gray-400 transition-transform flex-shrink-0 ${
            isOpen ? 'rotate-180' : ''
          }`}
        />
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 w-full min-w-[250px] max-w-[320px] bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 z-50 overflow-hidden">
          <div className="max-h-[300px] overflow-y-auto">
            {/* "All runs" option */}
            <button
              onClick={() => handleSelect('')}
              className={`
                w-full px-4 py-3 flex items-start gap-3 text-left transition-colors
                ${activeRunId === ''
                  ? 'bg-blue-50 dark:bg-blue-900/30'
                  : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                }
              `}
            >
              <List className={`w-4 h-4 mt-0.5 flex-shrink-0 ${
                activeRunId === ''
                  ? 'text-blue-600 dark:text-blue-400'
                  : 'text-gray-500 dark:text-gray-400'
              }`} />
              <div className="flex flex-col min-w-0 flex-1">
                <span className={`text-sm font-medium truncate ${
                  activeRunId === ''
                    ? 'text-blue-700 dark:text-blue-300'
                    : 'text-gray-900 dark:text-gray-100'
                }`}>
                  All runs
                </span>
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  Show agents from all runs
                </span>
              </div>
              {activeRunId === '' && (
                <Check className="w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
              )}
            </button>

            {/* Divider */}
            <div className="border-t border-gray-200 dark:border-gray-700" />

            {/* Run list */}
            {runs.map((run) => (
              <button
                key={run.id}
                onClick={() => handleSelect(run.id)}
                className={`
                  w-full px-4 py-3 flex items-start gap-3 text-left transition-colors
                  ${run.id === activeRunId
                    ? 'bg-blue-50 dark:bg-blue-900/30'
                    : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <div className="relative flex-shrink-0 mt-0.5">
                  <Clock className={`w-4 h-4 ${
                    run.id === activeRunId
                      ? 'text-blue-600 dark:text-blue-400'
                      : 'text-gray-500 dark:text-gray-400'
                  }`} />
                  <div className={`absolute -bottom-0.5 -right-0.5 w-2 h-2 rounded-full ${getStatusColor(run.status)}`} />
                </div>
                <div className="flex flex-col min-w-0 flex-1">
                  <span className={`text-sm font-medium truncate ${
                    run.id === activeRunId
                      ? 'text-blue-700 dark:text-blue-300'
                      : 'text-gray-900 dark:text-gray-100'
                  }`}>
                    {formatRunId(run.id)}
                  </span>
                  <span className="text-xs text-gray-500 dark:text-gray-400">
                    {formatDate(run.started_at)} · {run.completed_tasks}/{run.total_tasks} tasks
                  </span>
                </div>
                {run.id === activeRunId && (
                  <Check className="w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
                )}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
