import React, { useEffect, useState } from 'react';
import { Play, Pause, Activity, Clock, DollarSign, Zap, GitCommit, FileEdit, Sun, Moon, Terminal as TerminalIcon, List, ChevronUp, ChevronDown } from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';
import { useWebSocket } from '../../hooks/useWebSocket';
import { pauseOrch, resumeOrch, getState } from '../../api/client';
import { TaskList } from '../tasks/TaskList';
import { AgentCard } from '../agents/AgentCard';
import { AgentTerminal } from '../agents/AgentTerminal';
import { LiveFeed } from '../agents/LiveFeed';

type TerminalTab = 'feed' | 'terminal';

export const Dashboard: React.FC = () => {
  const { connected } = useWebSocket();
  const [isPauseLoading, setIsPauseLoading] = useState(false);
  const [isResumeLoading, setIsResumeLoading] = useState(false);
  const [activeTab, setActiveTab] = useState<TerminalTab>('feed');
  const [isDark, setIsDark] = useState(() => {
    const saved = localStorage.getItem('darkMode');
    if (saved !== null) return saved === 'true';
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  });
  const [isStatusExpanded, setIsStatusExpanded] = useState(() => {
    const saved = localStorage.getItem('statusPaneExpanded');
    if (saved !== null) return saved === 'true';
    return true; // Default to expanded
  });

  // Apply dark mode class to document
  useEffect(() => {
    if (isDark) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('darkMode', String(isDark));
  }, [isDark]);

  // Persist status pane expanded state
  useEffect(() => {
    localStorage.setItem('statusPaneExpanded', String(isStatusExpanded));
  }, [isStatusExpanded]);

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
    setSelectedAgent(agentId);
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
    <div className="flex flex-col h-screen bg-gray-50 dark:bg-gray-900">
      {/* Header */}
      <header className="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 px-6 py-4 flex-shrink-0">
        <div className="flex items-center justify-between">
          {/* Title and Connection Status */}
          <div className="flex items-center gap-4">
            <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">Canopy Dashboard</h1>
            <div className="flex items-center gap-2 px-3 py-1.5 bg-gray-100 dark:bg-gray-700 rounded-full">
              <div
                className={`w-2 h-2 rounded-full ${
                  connected ? 'bg-green-500 animate-pulse' : 'bg-red-500'
                }`}
              />
              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                {connected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
            <button
              onClick={() => setIsDark(!isDark)}
              className="p-2 rounded-lg bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
              title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
            >
              {isDark ? <Sun className="w-5 h-5 text-yellow-500" /> : <Moon className="w-5 h-5 text-gray-600" />}
            </button>
            <button
              onClick={() => setIsStatusExpanded(!isStatusExpanded)}
              className="p-2 rounded-lg bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
              title={isStatusExpanded ? 'Collapse status pane' : 'Expand status pane'}
            >
              {isStatusExpanded ? <ChevronUp className="w-5 h-5 text-gray-600 dark:text-gray-400" /> : <ChevronDown className="w-5 h-5 text-gray-600 dark:text-gray-400" />}
            </button>
          </div>

          {/* Pause/Resume and Stats Summary */}
          <div className="flex items-center gap-6">
            {/* Stats Summary */}
            <div className="flex items-center gap-4 text-sm">
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Activity className="w-4 h-4" />
                <span className="font-medium">{stats.running_tasks}</span>
                <span className="text-gray-500 dark:text-gray-500">running</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Zap className="w-4 h-4" />
                <span className="font-medium">{formatTokens(stats.total_tokens)}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
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

        {/* Extended Stats Bar - Collapsible */}
        <div
          className={`
            overflow-hidden transition-all duration-300 ease-in-out
            ${isStatusExpanded ? 'max-h-24 opacity-100 mt-4' : 'max-h-0 opacity-0 mt-0'}
          `}
        >
          <div className="grid grid-cols-6 gap-4">
            <div className="bg-gray-50 dark:bg-gray-700 rounded-lg px-4 py-2">
              <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Total Tasks</div>
              <div className="text-lg font-bold text-gray-900 dark:text-gray-100">{stats.total_tasks}</div>
            </div>
            <div className="bg-blue-50 dark:bg-blue-900/30 rounded-lg px-4 py-2">
              <div className="text-xs text-blue-600 dark:text-blue-400 mb-1">Running</div>
              <div className="text-lg font-bold text-blue-700 dark:text-blue-300">{stats.running_tasks}</div>
            </div>
            <div className="bg-green-50 dark:bg-green-900/30 rounded-lg px-4 py-2">
              <div className="text-xs text-green-600 dark:text-green-400 mb-1">Completed</div>
              <div className="text-lg font-bold text-green-700 dark:text-green-300">{stats.completed_tasks}</div>
            </div>
            <div className="bg-red-50 dark:bg-red-900/30 rounded-lg px-4 py-2">
              <div className="text-xs text-red-600 dark:text-red-400 mb-1">Failed</div>
              <div className="text-lg font-bold text-red-700 dark:text-red-300">{stats.failed_tasks}</div>
            </div>
            <div className="bg-purple-50 dark:bg-purple-900/30 rounded-lg px-4 py-2">
              <div className="text-xs text-purple-600 dark:text-purple-400 mb-1 flex items-center gap-1">
                <FileEdit className="w-3 h-3" />
                Changes
              </div>
              <div className="text-lg font-bold text-purple-700 dark:text-purple-300">{stats.file_changes}</div>
            </div>
            <div className="bg-indigo-50 dark:bg-indigo-900/30 rounded-lg px-4 py-2">
              <div className="text-xs text-indigo-600 dark:text-indigo-400 mb-1 flex items-center gap-1">
                <GitCommit className="w-3 h-3" />
                Commits
              </div>
              <div className="text-lg font-bold text-indigo-700 dark:text-indigo-300">{stats.git_commits}</div>
            </div>
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left Sidebar - Task List */}
        <aside className="w-80 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 flex-shrink-0 overflow-hidden">
          <TaskList tasks={tasks} onSelectTask={handleSelectTask} />
        </aside>

        {/* Main Content - Agent Grid and Terminal */}
        <main className="flex-1 flex flex-col overflow-hidden">
          {/* Agent Grid */}
          <div className="flex-1 overflow-y-auto p-6">
            {agentList.length === 0 ? (
              <div className="flex items-center justify-center h-full">
                <div className="text-center">
                  <Activity className="w-16 h-16 text-gray-300 dark:text-gray-600 mx-auto mb-4" />
                  <h3 className="text-lg font-medium text-gray-500 dark:text-gray-400 mb-2">No Active Agents</h3>
                  <p className="text-sm text-gray-400 dark:text-gray-500">
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
            <div className="h-80 border-t border-gray-200 dark:border-gray-700 bg-gray-900 flex-shrink-0">
              <div className="h-full flex flex-col">
                {/* Terminal Header with Tabs */}
                <div className="flex items-center justify-between px-4 py-2 bg-gray-800 border-b border-gray-700">
                  <div className="flex items-center gap-4">
                    {/* Tab buttons */}
                    <div className="flex items-center gap-1 bg-gray-900/50 rounded-lg p-1">
                      <button
                        onClick={() => setActiveTab('feed')}
                        className={`
                          flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                          ${activeTab === 'feed'
                            ? 'bg-blue-600 text-white'
                            : 'text-gray-400 hover:text-gray-200 hover:bg-gray-700/50'
                          }
                        `}
                      >
                        <List className="w-4 h-4" />
                        Live Feed
                      </button>
                      <button
                        onClick={() => setActiveTab('terminal')}
                        className={`
                          flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                          ${activeTab === 'terminal'
                            ? 'bg-blue-600 text-white'
                            : 'text-gray-400 hover:text-gray-200 hover:bg-gray-700/50'
                          }
                        `}
                      >
                        <TerminalIcon className="w-4 h-4" />
                        Raw Output
                      </button>
                    </div>

                    {/* Agent info */}
                    <div className="flex items-center gap-2 text-xs">
                      <span className="text-gray-500">|</span>
                      <code className="font-mono text-gray-400">
                        {selectedAgentId}
                      </code>
                      <span className="text-gray-500">-</span>
                      <span className="text-gray-400 truncate max-w-[200px]">
                        {agents[selectedAgentId]?.task_title}
                      </span>
                    </div>
                  </div>
                  <button
                    onClick={() => setSelectedAgent(null)}
                    className="text-gray-400 hover:text-gray-200 transition-colors px-2 py-1 rounded hover:bg-gray-700"
                  >
                    <span className="text-sm">Close</span>
                  </button>
                </div>

                {/* Tab Content */}
                <div className="flex-1 overflow-hidden">
                  {activeTab === 'feed' ? (
                    <LiveFeed agentId={selectedAgentId} />
                  ) : (
                    <AgentTerminal agentId={selectedAgentId} />
                  )}
                </div>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
};
