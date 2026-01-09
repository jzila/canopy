import React, { useEffect, useState } from 'react';
import { Play, Pause, Activity, Clock, DollarSign, Zap, GitCommit, FileEdit } from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';
import { useWebSocket } from '../../hooks/useWebSocket';
import { pauseOrch, resumeOrch, getState } from '../../api/client';
import { TaskList } from '../tasks/TaskList';
import { AgentCard } from '../agents/AgentCard';
import { AgentTerminal } from '../agents/AgentTerminal';

export const Dashboard: React.FC = () => {
  const { connected } = useWebSocket();
  const [isPauseLoading, setIsPauseLoading] = useState(false);
  const [isResumeLoading, setIsResumeLoading] = useState(false);

  // State from store
  const agents = useStateStore((state) => state.agents);
  const tasks = useStateStore((state) => state.tasks);
  const stats = useStateStore((state) => state.stats);
  const isPaused = useStateStore((state) => state.isPaused);
  const selectedAgentId = useStateStore((state) => state.selectedAgentId);
  const setSelectedAgent = useStateStore((state) => state.setSelectedAgent);
  const syncState = useStateStore((state) => state.syncState);
  const setIsPaused = useStateStore((state) => state.setIsPaused);

  // Load initial state on mount
  useEffect(() => {
    const loadInitialState = async () => {
      try {
        const state = await getState();
        syncState(state);
      } catch (error) {
        console.error('Failed to load initial state:', error);
      }
    };

    loadInitialState();
  }, [syncState]);

  const handlePause = async () => {
    if (isPauseLoading) return;

    try {
      setIsPauseLoading(true);
      await pauseOrch();
      setIsPaused(true);
    } catch (error) {
      console.error('Failed to pause orchestration:', error);
    } finally {
      setIsPauseLoading(false);
    }
  };

  const handleResume = async () => {
    if (isResumeLoading) return;

    try {
      setIsResumeLoading(true);
      await resumeOrch();
      setIsPaused(false);
    } catch (error) {
      console.error('Failed to resume orchestration:', error);
    } finally {
      setIsResumeLoading(false);
    }
  };

  const handleSelectAgent = (agentId: string) => {
    setSelectedAgent(agentId === selectedAgentId ? null : agentId);
  };

  const handleSelectTask = (taskId: string) => {
    // Find agent assigned to this task
    const task = tasks[taskId];
    if (task?.agent_id) {
      setSelectedAgent(task.agent_id);
    }
  };

  const agentList = Object.values(agents);
  const hasSelectedAgent = selectedAgentId && agents[selectedAgentId];

  const formatCost = (cost: number): string => {
    if (cost < 0.01) {
      return `$${(cost * 100).toFixed(2)}c`;
    }
    return `$${cost.toFixed(2)}`;
  };

  const formatDuration = (duration: number): string => {
    const hours = Math.floor(duration / 3600);
    const minutes = Math.floor((duration % 3600) / 60);
    const seconds = Math.floor(duration % 60);

    if (hours > 0) {
      return `${hours}h ${minutes}m`;
    } else if (minutes > 0) {
      return `${minutes}m ${seconds}s`;
    } else {
      return `${seconds}s`;
    }
  };

  const formatTokens = (tokens: number): string => {
    if (tokens >= 1000000) {
      return `${(tokens / 1000000).toFixed(1)}M`;
    } else if (tokens >= 1000) {
      return `${(tokens / 1000).toFixed(1)}K`;
    }
    return tokens.toString();
  };

  return (
    <div className="flex flex-col h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 px-6 py-4 flex-shrink-0">
        <div className="flex items-center justify-between">
          {/* Title and Connection Status */}
          <div className="flex items-center gap-4">
            <h1 className="text-2xl font-bold text-gray-900">Canopy Dashboard</h1>
            <div className="flex items-center gap-2 px-3 py-1.5 bg-gray-100 rounded-full">
              <div
                className={`w-2 h-2 rounded-full ${
                  connected ? 'bg-green-500 animate-pulse' : 'bg-red-500'
                }`}
              />
              <span className="text-sm font-medium text-gray-700">
                {connected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
          </div>

          {/* Pause/Resume and Stats Summary */}
          <div className="flex items-center gap-6">
            {/* Stats Summary */}
            <div className="flex items-center gap-4 text-sm">
              <div className="flex items-center gap-1.5 text-gray-600">
                <Activity className="w-4 h-4" />
                <span className="font-medium">{stats.running_tasks}</span>
                <span className="text-gray-500">running</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600">
                <Zap className="w-4 h-4" />
                <span className="font-medium">{formatTokens(stats.total_tokens)}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600">
                <DollarSign className="w-4 h-4" />
                <span className="font-medium">{formatCost(stats.total_cost_usd)}</span>
              </div>
            </div>

            {/* Pause/Resume Button */}
            {isPaused ? (
              <button
                onClick={handleResume}
                disabled={isResumeLoading || !connected}
                className={`
                  flex items-center gap-2 px-4 py-2 bg-green-500 text-white rounded-lg
                  font-medium transition-colors
                  ${
                    isResumeLoading || !connected
                      ? 'opacity-50 cursor-not-allowed'
                      : 'hover:bg-green-600'
                  }
                `}
              >
                <Play className="w-4 h-4" />
                {isResumeLoading ? 'Resuming...' : 'Resume'}
              </button>
            ) : (
              <button
                onClick={handlePause}
                disabled={isPauseLoading || !connected}
                className={`
                  flex items-center gap-2 px-4 py-2 bg-orange-500 text-white rounded-lg
                  font-medium transition-colors
                  ${
                    isPauseLoading || !connected
                      ? 'opacity-50 cursor-not-allowed'
                      : 'hover:bg-orange-600'
                  }
                `}
              >
                <Pause className="w-4 h-4" />
                {isPauseLoading ? 'Pausing...' : 'Pause'}
              </button>
            )}
          </div>
        </div>

        {/* Extended Stats Bar */}
        <div className="mt-4 grid grid-cols-6 gap-4">
          <div className="bg-gray-50 rounded-lg px-4 py-2">
            <div className="text-xs text-gray-500 mb-1">Total Tasks</div>
            <div className="text-lg font-bold text-gray-900">{stats.total_tasks}</div>
          </div>
          <div className="bg-blue-50 rounded-lg px-4 py-2">
            <div className="text-xs text-blue-600 mb-1">Running</div>
            <div className="text-lg font-bold text-blue-700">{stats.running_tasks}</div>
          </div>
          <div className="bg-green-50 rounded-lg px-4 py-2">
            <div className="text-xs text-green-600 mb-1">Completed</div>
            <div className="text-lg font-bold text-green-700">{stats.completed_tasks}</div>
          </div>
          <div className="bg-red-50 rounded-lg px-4 py-2">
            <div className="text-xs text-red-600 mb-1">Failed</div>
            <div className="text-lg font-bold text-red-700">{stats.failed_tasks}</div>
          </div>
          <div className="bg-purple-50 rounded-lg px-4 py-2">
            <div className="text-xs text-purple-600 mb-1 flex items-center gap-1">
              <FileEdit className="w-3 h-3" />
              Changes
            </div>
            <div className="text-lg font-bold text-purple-700">{stats.file_changes}</div>
          </div>
          <div className="bg-indigo-50 rounded-lg px-4 py-2">
            <div className="text-xs text-indigo-600 mb-1 flex items-center gap-1">
              <GitCommit className="w-3 h-3" />
              Commits
            </div>
            <div className="text-lg font-bold text-indigo-700">{stats.git_commits}</div>
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left Sidebar - Task List */}
        <aside className="w-80 bg-white border-r border-gray-200 flex-shrink-0 overflow-hidden">
          <TaskList tasks={tasks} onSelectTask={handleSelectTask} />
        </aside>

        {/* Main Content - Agent Grid and Terminal */}
        <main className="flex-1 flex flex-col overflow-hidden">
          {/* Agent Grid */}
          <div className="flex-1 overflow-y-auto p-6">
            {agentList.length === 0 ? (
              <div className="flex items-center justify-center h-full">
                <div className="text-center">
                  <Activity className="w-16 h-16 text-gray-300 mx-auto mb-4" />
                  <h3 className="text-lg font-medium text-gray-500 mb-2">No Active Agents</h3>
                  <p className="text-sm text-gray-400">
                    Agents will appear here when tasks are running
                  </p>
                </div>
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                {agentList.map((agent) => (
                  <AgentCard
                    key={agent.id}
                    agent={agent}
                    onSelect={handleSelectAgent}
                    isSelected={agent.id === selectedAgentId}
                  />
                ))}
              </div>
            )}
          </div>

          {/* Bottom Terminal Panel */}
          {hasSelectedAgent && (
            <div className="h-80 border-t border-gray-200 bg-gray-900 flex-shrink-0">
              <div className="h-full flex flex-col">
                {/* Terminal Header */}
                <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-medium text-gray-300">Terminal</span>
                    <span className="text-xs text-gray-500">|</span>
                    <code className="text-xs font-mono text-gray-400">
                      {selectedAgentId}
                    </code>
                    <span className="text-xs text-gray-500">|</span>
                    <span className="text-xs text-gray-400">
                      {agents[selectedAgentId]?.task_title}
                    </span>
                  </div>
                  <button
                    onClick={() => setSelectedAgent(null)}
                    className="text-gray-400 hover:text-gray-200 transition-colors"
                  >
                    <span className="text-sm">Close</span>
                  </button>
                </div>

                {/* Terminal Content */}
                <div className="flex-1 overflow-hidden">
                  <AgentTerminal agentId={selectedAgentId} />
                </div>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
};
