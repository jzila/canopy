import React from 'react';
import { Loader2 } from 'lucide-react';
import type { OrchestratorState, PauseState } from '../../stores/stateStore';

export interface OrchestratorStateIndicatorProps {
  /** Current orchestrator state */
  orchestratorState: OrchestratorState;
  /** Whether connected to the backend */
  connected: boolean;
  /** Current pause state (for showing paused_user vs paused_agent) */
  pauseState: PauseState;
  /** Loading states */
  isActivating: boolean;
  isDeactivating: boolean;
  isPauseLoading: boolean;
  isResumeLoading: boolean;
  /** Event handlers */
  onActivate: () => void;
  onDeactivate: () => void;
  onPause: () => void;
  onResume: () => void;
  onConfigure: () => void;
}

/**
 * Orchestrator action buttons component that provides controls to
 * activate, deactivate, pause, and resume the orchestrator.
 * The state indicator is now shown as a dot on the RepoSelector.
 */
export const OrchestratorStateIndicator: React.FC<OrchestratorStateIndicatorProps> = ({
  orchestratorState,
  connected,
  pauseState,
  isActivating,
  isDeactivating,
  isPauseLoading,
  isResumeLoading,
  onActivate,
  onDeactivate,
  onPause,
  onResume,
  onConfigure,
}) => {
  const isLoading = isActivating || isDeactivating || isPauseLoading || isResumeLoading;
  const disabled = !connected || isLoading;

  // Determine which buttons to show based on state
  // Hide Activate while activating to prevent "double button" appearance during transition
  // Hide other controls while deactivating since we're transitioning to 'off'
  const showActivateButton = orchestratorState === 'off' && !isActivating;
  const showConfigureButton = orchestratorState === 'off' && !isActivating;
  const showPauseButton = (orchestratorState === 'idle' || orchestratorState === 'active') && !isDeactivating;
  const showResumeButton = orchestratorState === 'paused' && !isDeactivating;
  const showDeactivateButton = orchestratorState !== 'off' && !isDeactivating;

  // Can only resume if paused by user (not just agent)
  const canResume = pauseState === 'paused_user' || pauseState === 'paused_both';

  return (
    <div className="flex items-center gap-2">
        {showActivateButton && (
          <button
            onClick={onActivate}
            disabled={disabled}
            title={!connected ? 'Not connected to server' : 'Activate orchestrator'}
            className={`
              flex items-center gap-2 px-4 h-12 text-white rounded-lg
              font-medium transition-colors
              ${disabled
                ? 'bg-green-400 dark:bg-green-600 opacity-50 cursor-not-allowed'
                : 'bg-green-500 hover:bg-green-600'
              }
            `}
          >
            {isActivating ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Activating...</span>
              </>
            ) : (
              <span className="text-sm">Activate</span>
            )}
          </button>
        )}

        {showConfigureButton && (
          <button
            onClick={onConfigure}
            disabled={disabled}
            title={!connected ? 'Not connected to server' : 'Configure run settings'}
            className={`
              flex items-center gap-2 px-4 h-12 rounded-lg
              font-medium transition-colors border
              ${disabled
                ? 'border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500 opacity-50 cursor-not-allowed'
                : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
              }
            `}
          >
            <span className="text-sm">Configure</span>
          </button>
        )}

        {showPauseButton && (
          <button
            onClick={onPause}
            disabled={disabled}
            title={!connected ? 'Not connected to server' : 'Pause spawning new agents'}
            className={`
              flex items-center gap-2 px-4 h-12 text-white rounded-lg
              font-medium transition-colors
              ${disabled
                ? 'bg-orange-400 dark:bg-orange-600 opacity-50 cursor-not-allowed'
                : 'bg-orange-500 hover:bg-orange-600'
              }
            `}
          >
            {isPauseLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Pausing...</span>
              </>
            ) : (
              <span className="text-sm">Pause</span>
            )}
          </button>
        )}

        {showResumeButton && (
          <button
            onClick={onResume}
            disabled={disabled || !canResume}
            title={
              !connected
                ? 'Not connected to server'
                : !canResume
                  ? 'Orchestrator paused by agent - will auto-resume when complete'
                  : 'Resume spawning new agents'
            }
            className={`
              flex items-center gap-2 px-4 h-12 text-white rounded-lg
              font-medium transition-colors
              ${disabled || !canResume
                ? 'bg-green-400 dark:bg-green-600 opacity-50 cursor-not-allowed'
                : 'bg-green-500 hover:bg-green-600'
              }
            `}
          >
            {isResumeLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Resuming...</span>
              </>
            ) : (
              <span className="text-sm">Resume</span>
            )}
          </button>
        )}

        {showDeactivateButton && (
          <button
            onClick={onDeactivate}
            disabled={disabled}
            title={!connected ? 'Not connected to server' : 'Stop the orchestrator'}
            className={`
              flex items-center gap-2 px-4 h-12 text-white rounded-lg
              font-medium transition-colors
              ${disabled
                ? 'bg-red-400 dark:bg-red-600 opacity-50 cursor-not-allowed'
                : 'bg-red-500 hover:bg-red-600'
              }
            `}
          >
            {isDeactivating ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Stopping...</span>
              </>
            ) : (
              <span className="text-sm">Deactivate</span>
            )}
          </button>
        )}
    </div>
  );
};
