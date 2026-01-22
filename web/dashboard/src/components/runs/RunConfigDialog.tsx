import React, { useState, useEffect, useRef } from 'react';
import { X, Settings, Loader2 } from 'lucide-react';
import type { RunConfig } from '../../stores/stateStore';
import { DEFAULT_RUN_CONFIG } from '../../stores/stateStore';

interface RunConfigDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onStart: (config: RunConfig) => void;
  isStarting: boolean;
  initialConfig?: RunConfig;
  repoName?: string | undefined;
}

export const RunConfigDialog: React.FC<RunConfigDialogProps> = ({
  isOpen,
  onClose,
  onStart,
  isStarting,
  initialConfig = DEFAULT_RUN_CONFIG,
  repoName,
}) => {
  const [config, setConfig] = useState<RunConfig>(initialConfig);
  const dialogRef = useRef<HTMLDivElement>(null);

  // Reset config when dialog opens
  useEffect(() => {
    if (isOpen) {
      setConfig(initialConfig);
    }
  }, [isOpen, initialConfig]);

  // Handle escape key
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen && !isStarting) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, isStarting, onClose]);

  // Handle click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        dialogRef.current &&
        !dialogRef.current.contains(event.target as Node) &&
        !isStarting
      ) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
    }
  }, [isOpen, isStarting, onClose]);

  if (!isOpen) return null;

  const handleStart = () => {
    onStart(config);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        ref={dialogRef}
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <Settings className="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100">
              Start Run
            </h2>
          </div>
          <button
            onClick={onClose}
            disabled={isStarting}
            className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5 text-gray-500 dark:text-gray-400" />
          </button>
        </div>

        {/* Content */}
        <div className="px-6 py-4 space-y-5">
          {repoName && (
            <div className="text-sm text-gray-600 dark:text-gray-400">
              Repository: <span className="font-medium text-gray-900 dark:text-gray-100">{repoName}</span>
            </div>
          )}

          {/* Concurrency */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Concurrency
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="1"
                max="16"
                value={config.concurrency}
                onChange={(e) => setConfig({ ...config, concurrency: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                {config.concurrency}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Number of agents running in parallel
            </p>
          </div>

          {/* Max Priority */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Max Priority (P0-P4)
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="0"
                max="4"
                value={config.max_priority}
                onChange={(e) => setConfig({ ...config, max_priority: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                P{config.max_priority}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Only process tasks up to this priority level
            </p>
          </div>

          {/* Max Retries */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Max Retries
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="0"
                max="5"
                value={config.max_retries}
                onChange={(e) => setConfig({ ...config, max_retries: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                {config.max_retries}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Retry failed tasks up to this many times
            </p>
          </div>

          {/* Sandbox Toggle */}
          <div className="flex items-center justify-between">
            <div>
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Sandbox Mode (bwrap)
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Isolate agents in sandboxed environments
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={config.use_bwrap}
              onClick={() => setConfig({ ...config, use_bwrap: !config.use_bwrap })}
              className={`
                relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent
                transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2
                ${config.use_bwrap ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'}
              `}
            >
              <span
                className={`
                  pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0
                  transition duration-200 ease-in-out
                  ${config.use_bwrap ? 'translate-x-5' : 'translate-x-0'}
                `}
              />
            </button>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={onClose}
            disabled={isStarting}
            className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={handleStart}
            disabled={isStarting}
            className="px-4 py-2 text-sm font-medium text-white bg-green-600 rounded-lg hover:bg-green-700 transition-colors disabled:opacity-50 flex items-center gap-2"
          >
            {isStarting ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Starting...
              </>
            ) : (
              'Start Run'
            )}
          </button>
        </div>
      </div>
    </div>
  );
};
