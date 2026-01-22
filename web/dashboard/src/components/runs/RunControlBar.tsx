import React from 'react';
import { Play, Square, Pause, Loader2 } from 'lucide-react';
import type { PauseState } from '../../stores/stateStore';

export interface RunControlBarProps {
  /** Whether there's an active run */
  hasActiveRun: boolean;
  /** Current run ID (for display) */
  currentRunId: string;
  /** Whether connected to the backend */
  connected: boolean;
  /** Whether the orchestrator is paused */
  isPaused: boolean;
  /** Whether paused by agent (auto-pause) */
  isPausedByAgent: boolean;
  /** Current pause state */
  pauseState: PauseState;
  /** Loading states */
  isPauseLoading: boolean;
  isResumeLoading: boolean;
  isStartingRun: boolean;
  isStoppingRun: boolean;
  /** Event handlers */
  onPause: () => void;
  onResume: () => void;
  onStartRun: () => void;
  onStopRun: () => void;
}

/**
 * Get the display text for the pause button based on pause state
 */
function getPauseButtonText(
  pauseState: PauseState,
  isPauseLoading: boolean,
  isResumeLoading: boolean
): string {
  if (isPauseLoading) return 'Pausing...';
  if (isResumeLoading) return 'Resuming...';

  switch (pauseState) {
    case 'paused_user':
      return 'Paused';
    case 'paused_agent':
      return 'Agent Active...';
    case 'paused_both':
      return 'Paused (agent)';
    default:
      return 'Pause';
  }
}

/**
 * Get the tooltip text for the pause/resume button
 */
function getPauseButtonTooltip(
  pauseState: PauseState,
  isPaused: boolean,
  hasActiveRun: boolean
): string {
  if (!hasActiveRun) {
    return 'No active run';
  }
  if (isPaused) {
    switch (pauseState) {
      case 'paused_user':
        return 'Orchestrator paused by user. Click to resume spawning new agents.';
      case 'paused_agent':
        return 'Orchestrator paused while agent is active (resolving conflicts or repairing). Will auto-resume when complete.';
      case 'paused_both':
        return 'Orchestrator paused by user while agent is active. Click to allow new agents after agent completes.';
      default:
        return 'Click to resume spawning new agents';
    }
  }
  return 'Pause spawning of new agents. Running agents will continue until completion.';
}

/**
 * Run control bar component that provides Start/Stop and Pause/Resume controls.
 * This component is designed to be placed in the status filter bar area.
 */
export const RunControlBar: React.FC<RunControlBarProps> = ({
  hasActiveRun,
  currentRunId: _currentRunId,
  connected,
  isPaused,
  isPausedByAgent,
  pauseState,
  isPauseLoading,
  isResumeLoading,
  isStartingRun,
  isStoppingRun,
  onPause,
  onResume,
  onStartRun,
  onStopRun,
}) => {
  const buttonText = getPauseButtonText(pauseState, isPauseLoading, isResumeLoading);
  const buttonTooltip = getPauseButtonTooltip(pauseState, isPaused, hasActiveRun);
  const isPauseDisabled = !hasActiveRun || !connected;

  // Start Run Button
  const StartButton = () => (
    <button
      onClick={onStartRun}
      disabled={!connected || isStartingRun || hasActiveRun}
      title={
        hasActiveRun
          ? 'A run is already active'
          : !connected
            ? 'Not connected to server'
            : 'Start a new orchestration run'
      }
      className={`
        flex items-center gap-2 px-4 h-12 text-white rounded-lg
        font-medium transition-colors
        ${hasActiveRun || !connected || isStartingRun
          ? 'bg-green-400 dark:bg-green-600 opacity-50 cursor-not-allowed'
          : 'bg-green-500 hover:bg-green-600'
        }
      `}
    >
      {isStartingRun ? (
        <>
          <Loader2 className="w-4 h-4 animate-spin" />
          <span className="text-sm">Starting...</span>
        </>
      ) : (
        <>
          <Play className="w-4 h-4" />
          <span className="text-sm">Start Run</span>
        </>
      )}
    </button>
  );

  // Stop Run Button
  const StopButton = () => (
    <button
      onClick={onStopRun}
      disabled={!hasActiveRun || !connected || isStoppingRun}
      title={
        !hasActiveRun
          ? 'No active run to stop'
          : !connected
            ? 'Not connected to server'
            : 'Stop the current run'
      }
      className={`
        flex items-center gap-2 px-4 h-12 text-white rounded-lg
        font-medium transition-colors
        ${!hasActiveRun || !connected || isStoppingRun
          ? 'bg-red-400 dark:bg-red-600 opacity-50 cursor-not-allowed'
          : 'bg-red-500 hover:bg-red-600'
        }
      `}
    >
      {isStoppingRun ? (
        <>
          <Loader2 className="w-4 h-4 animate-spin" />
          <span className="text-sm">Stopping...</span>
        </>
      ) : (
        <>
          <Square className="w-4 h-4" />
          <span className="text-sm">Stop Run</span>
        </>
      )}
    </button>
  );

  // Pause/Resume Button
  const PauseResumeButton = () => {
    if (isPaused) {
      return (
        <button
          onClick={onResume}
          disabled={isPauseDisabled || isResumeLoading || (isPausedByAgent && pauseState === 'paused_agent')}
          title={buttonTooltip}
          className={`
            flex items-center gap-2 px-4 h-12 text-white rounded-lg
            font-medium transition-colors
            ${pauseState === 'paused_agent' ? 'bg-blue-500' : 'bg-green-500'}
            ${
              isPauseDisabled || isResumeLoading || (isPausedByAgent && pauseState === 'paused_agent')
                ? 'opacity-50 cursor-not-allowed'
                : pauseState === 'paused_agent'
                  ? 'hover:bg-blue-600'
                  : 'hover:bg-green-600'
            }
          `}
        >
          <Play className="w-4 h-4" />
          <span className="text-sm">{buttonText}</span>
        </button>
      );
    }

    return (
      <button
        onClick={onPause}
        disabled={isPauseDisabled || isPauseLoading}
        title={buttonTooltip}
        className={`
          flex items-center gap-2 px-4 h-12 bg-orange-500 text-white rounded-lg
          font-medium transition-colors
          ${
            isPauseDisabled || isPauseLoading
              ? 'opacity-50 cursor-not-allowed'
              : 'hover:bg-orange-600'
          }
        `}
      >
        <Pause className="w-4 h-4" />
        <span className="text-sm">{buttonText}</span>
      </button>
    );
  };

  return (
    <div className="flex items-center gap-2">
      {/* Show Start or Stop based on active run status */}
      {hasActiveRun ? (
        <>
          <StopButton />
          <PauseResumeButton />
        </>
      ) : (
        <StartButton />
      )}
    </div>
  );
};
