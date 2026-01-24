import React from 'react';
import { Loader2 } from 'lucide-react';
import type { OrchestratorState, PauseState } from '../../stores/stateStore';

export interface OrchestratorStateIndicatorProps {
  /** Current orchestrator state */
  orchestratorState: OrchestratorState;
  /** Number of currently active agents */
  activeAgentCount: number;
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
}

/**
 * Get the state indicator icon for the current orchestrator state.
 */
function getStateIcon(state: OrchestratorState): string {
  switch (state) {
    case 'off':
      return '\u25CB'; // White circle
    case 'idle':
      return '\u25D0'; // Circle with left half black
    case 'active':
      return '\u25CF'; // Black circle
    case 'paused':
      return '\u23F8'; // Pause symbol
    default:
      return '\u25CB';
  }
}

/**
 * Get the display text for the current orchestrator state.
 */
function getStateText(
  state: OrchestratorState,
  activeAgentCount: number,
  pauseState: PauseState
): string {
  switch (state) {
    case 'off':
      return 'Off';
    case 'idle':
      return 'Idle (watching)';
    case 'active':
      return `Active (${activeAgentCount} agent${activeAgentCount !== 1 ? 's' : ''})`;
    case 'paused':
      if (pauseState === 'paused_agent') {
        return 'Paused (agent)';
      } else if (pauseState === 'paused_both') {
        return 'Paused (user+agent)';
      }
      return 'Paused';
    default:
      return 'Unknown';
  }
}

/**
 * Get the color classes for the state indicator.
 */
function getStateColorClasses(state: OrchestratorState): string {
  switch (state) {
    case 'off':
      return 'text-gray-400 dark:text-gray-500';
    case 'idle':
      return 'text-blue-500 dark:text-blue-400';
    case 'active':
      return 'text-green-500 dark:text-green-400';
    case 'paused':
      return 'text-orange-500 dark:text-orange-400';
    default:
      return 'text-gray-400 dark:text-gray-500';
  }
}

/**
 * Orchestrator state indicator component that displays the current state
 * and provides controls to activate, deactivate, pause, and resume.
 */
export const OrchestratorStateIndicator: React.FC<OrchestratorStateIndicatorProps> = ({
  orchestratorState,
  activeAgentCount,
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
}) => {
  // Use transitioning icon/text during activation/deactivation for visual feedback
  const stateIcon = isActivating || isDeactivating ? '\u25D4' : getStateIcon(orchestratorState); // Half-filled circle during transitions
  const stateText = isActivating
    ? 'Activating...'
    : isDeactivating
      ? 'Deactivating...'
      : getStateText(orchestratorState, activeAgentCount, pauseState);
  const stateColorClasses = isActivating || isDeactivating
    ? 'text-yellow-500 dark:text-yellow-400'
    : getStateColorClasses(orchestratorState);

  const isLoading = isActivating || isDeactivating || isPauseLoading || isResumeLoading;
  const disabled = !connected || isLoading;

  // Determine which buttons to show based on state
  // Hide Activate while activating to prevent "double button" appearance during transition
  // Hide other controls while deactivating since we're transitioning to 'off'
  const showActivateButton = orchestratorState === 'off' && !isActivating;
  const showPauseButton = (orchestratorState === 'idle' || orchestratorState === 'active') && !isDeactivating;
  const showResumeButton = orchestratorState === 'paused' && !isDeactivating;
  const showDeactivateButton = orchestratorState !== 'off' && !isDeactivating;

  // Can only resume if paused by user (not just agent)
  const canResume = pauseState === 'paused_user' || pauseState === 'paused_both';

  return (
    <div className="flex items-center gap-3">
      {/* State indicator */}
      <div className={`flex items-center gap-2 px-4 h-12 bg-gray-100 dark:bg-gray-700 rounded-lg ${stateColorClasses}`}>
        <span className="text-lg leading-none">{stateIcon}</span>
        <span className="text-sm font-medium tracking-wide text-gray-700 dark:text-gray-300">
          {stateText}
        </span>
      </div>

      {/* Action buttons */}
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
    </div>
  );
};
