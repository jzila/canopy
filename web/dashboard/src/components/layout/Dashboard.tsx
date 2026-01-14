import React, { useEffect, useState, useMemo, useCallback, useRef } from 'react';
import { Play, Pause, Activity, DollarSign, Zap, GitCommit, FileEdit, Sun, Moon, Terminal as TerminalIcon, List, CheckCircle, XCircle, ListTodo, GripHorizontal, Archive, GitMerge, ChevronDown, ChevronUp } from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';
import type { AgentState } from '../../stores/stateStore';
import { useWebSocket, useMergeQueue } from '../../hooks';
import { pauseOrch, resumeOrch, getState, getRepositories, activateRepository } from '../../api/client';
import { AgentCardGroup } from '../agents/AgentCardGroup';
import { AgentTerminal } from '../agents/AgentTerminal';
import { LiveFeed } from '../agents/LiveFeed';
import { CommitList } from '../agents/CommitList';
import { BeadsPane } from '../beads/BeadsPane';
import { RepoSelector } from './RepoSelector';
import { MergeQueueTree } from '../merge';
import type { NodeDetails } from '../merge/types';

type TerminalTab = 'feed' | 'terminal' | 'commits';
type StatusFilter = 'all' | 'running' | 'completed' | 'failed';

const SHOW_ARCHIVED_AGENTS_KEY = 'canopy-show-archived-agents';

const MERGE_QUEUE_EXPANDED_KEY = 'canopy-merge-queue-expanded';

export const Dashboard: React.FC = () => {
  const { connected } = useWebSocket();
  const { mergeQueue } = useMergeQueue();
  const [isPauseLoading, setIsPauseLoading] = useState(false);
  const [isResumeLoading, setIsResumeLoading] = useState(false);
  const [activeTab, setActiveTab] = useState<TerminalTab>('feed');
  const [isDark, setIsDark] = useState(() => {
    const saved = localStorage.getItem('darkMode');
    if (saved !== null) return saved === 'true';
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  });
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [beadsPaneExpanded, setBeadsPaneExpanded] = useState(() => {
    const saved = localStorage.getItem('beadsPaneExpanded');
    return saved !== null ? saved === 'true' : true;
  });
  const [showArchivedAgents, setShowArchivedAgents] = useState(() => {
    const saved = localStorage.getItem(SHOW_ARCHIVED_AGENTS_KEY);
    return saved === 'true';
  });
  const [mergeQueueExpanded, setMergeQueueExpanded] = useState(() => {
    const saved = localStorage.getItem(MERGE_QUEUE_EXPANDED_KEY);
    return saved !== null ? saved === 'true' : true;
  });

  // Persist beads pane state
  useEffect(() => {
    localStorage.setItem('beadsPaneExpanded', String(beadsPaneExpanded));
  }, [beadsPaneExpanded]);

  // Persist show archived agents state
  useEffect(() => {
    localStorage.setItem(SHOW_ARCHIVED_AGENTS_KEY, String(showArchivedAgents));
  }, [showArchivedAgents]);

  // Persist merge queue expanded state
  useEffect(() => {
    localStorage.setItem(MERGE_QUEUE_EXPANDED_KEY, String(mergeQueueExpanded));
  }, [mergeQueueExpanded]);

  // Resizable pane state
  const MIN_PANE_HEIGHT = 200;
  const MAX_PANE_HEIGHT_RATIO = 0.8; // 80% of viewport
  const DEFAULT_PANE_HEIGHT = 320;

  const [paneHeight, setPaneHeight] = useState(() => {
    const saved = localStorage.getItem('outputPaneHeight');
    return saved ? parseInt(saved, 10) : DEFAULT_PANE_HEIGHT;
  });
  const [isResizing, setIsResizing] = useState(false);
  const resizeRef = useRef<{ startY: number; startHeight: number } | null>(null);

  // Persist pane height
  useEffect(() => {
    localStorage.setItem('outputPaneHeight', String(paneHeight));
  }, [paneHeight]);

  // Handle resize mouse events
  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsResizing(true);
    resizeRef.current = {
      startY: e.clientY,
      startHeight: paneHeight,
    };
  }, [paneHeight]);

  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      if (!resizeRef.current) return;

      const deltaY = resizeRef.current.startY - e.clientY;
      const maxHeight = window.innerHeight * MAX_PANE_HEIGHT_RATIO;
      const newHeight = Math.min(
        maxHeight,
        Math.max(MIN_PANE_HEIGHT, resizeRef.current.startHeight + deltaY)
      );
      setPaneHeight(newHeight);
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
  }, [isResizing]);

  // Apply dark mode class to document
  useEffect(() => {
    if (isDark) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('darkMode', String(isDark));
  }, [isDark]);

  // State from store
  const agents = useStateStore((state) => state.agents);
  const stats = useStateStore((state) => state.stats);
  const isPaused = useStateStore((state) => state.isPaused);
  const selectedAgentId = useStateStore((state) => state.selectedAgentId);
  const setSelectedAgent = useStateStore((state) => state.setSelectedAgent);
  const syncState = useStateStore((state) => state.syncState);
  const setIsPaused = useStateStore((state) => state.setIsPaused);
  const updateAgent = useStateStore((state) => state.updateAgent);
  const repositories = useStateStore((state) => state.repositories);
  const activeRepoId = useStateStore((state) => state.activeRepoId);
  const isRepoSwitching = useStateStore((state) => state.isRepoSwitching);
  const setRepositories = useStateStore((state) => state.setRepositories);
  const setActiveRepo = useStateStore((state) => state.setActiveRepo);
  const setRepoSwitching = useStateStore((state) => state.setRepoSwitching);

  // Load initial state on mount
  useEffect(() => {
    const loadInitialState = async () => {
      try {
        const [state, repoResponse] = await Promise.all([
          getState(),
          getRepositories(),
        ]);
        syncState(state);

        // If repositories exist but no active repo is set, auto-select the first one
        let activeId = repoResponse.active_repo_id;
        if (!activeId && repoResponse.repositories.length > 0) {
          const firstRepo = repoResponse.repositories[0]!;
          activeId = firstRepo.id;
          // Activate the first repository on the backend
          try {
            await activateRepository(firstRepo.id);
          } catch (activateError) {
            console.error('Failed to auto-activate repository:', activateError);
            // Continue with local activeId even if backend activation fails
            // The selector will still show the correct repo
          }
        }

        // Always update state with repositories, even if activation failed
        // This ensures the selector is visible and functional
        setRepositories(repoResponse.repositories, activeId);
      } catch (error) {
        console.error('Failed to load initial state:', error);
        // Attempt to load repositories separately if combined fetch failed
        try {
          const repoResponse = await getRepositories();
          let activeId = repoResponse.active_repo_id;
          if (!activeId && repoResponse.repositories.length > 0) {
            activeId = repoResponse.repositories[0]!.id;
          }
          setRepositories(repoResponse.repositories, activeId);
        } catch (repoError) {
          console.error('Failed to load repositories:', repoError);
        }
      }
    };

    loadInitialState();
  }, [syncState, setRepositories]);

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

  const handleRepoSelect = async (repoId: string) => {
    if (isRepoSwitching || repoId === activeRepoId) return;

    try {
      setRepoSwitching(true);
      await activateRepository(repoId);
      setActiveRepo(repoId);
      // After switching repos, reload state to get the new repo's data
      const state = await getState();
      syncState(state);
    } catch (error) {
      console.error('Failed to switch repository:', error);
    } finally {
      setRepoSwitching(false);
    }
  };

  const handleSelectAgent = (agentId: string) => {
    setSelectedAgent(agentId);
  };

  const agentList = Object.values(agents);

  // Count archived agents
  const archivedAgentCount = useMemo(() => {
    return agentList.filter(agent => agent.archived).length;
  }, [agentList]);

  // Filter agents based on selected status and archived state
  const filteredAgents = useMemo(() => {
    return agentList
      .filter(agent => {
        // Filter by archived status first
        if (!showArchivedAgents && agent.archived) {
          return false;
        }

        // Then filter by status
        if (statusFilter === 'all') return true;

        switch (statusFilter) {
          case 'running':
            return agent.status === 'running' || agent.status === 'starting';
          case 'completed':
            return agent.status === 'completed';
          case 'failed':
            return agent.status === 'failed' || agent.status === 'timed_out' || agent.status === 'cancelled';
          default:
            return true;
        }
      })
      .sort((a, b) => {
        // Archived agents go to the bottom
        if (a.archived !== b.archived) {
          return a.archived ? 1 : -1;
        }
        // Sort by start time (most recent first)
        return new Date(b.start_time).getTime() - new Date(a.start_time).getTime();
      });
  }, [agentList, statusFilter, showArchivedAgents]);

  // Group agents by parent/child relationships
  // Returns parent agents with their children, excluding standalone child agents
  const groupedAgents = useMemo(() => {
    // Build a map of parent_id -> children
    const childrenByParent = new Map<string, AgentState[]>();
    const childIds = new Set<string>();

    for (const agent of filteredAgents) {
      if (agent.parent_agent_id) {
        childIds.add(agent.id);
        const siblings = childrenByParent.get(agent.parent_agent_id) || [];
        siblings.push(agent);
        childrenByParent.set(agent.parent_agent_id, siblings);
      }
    }

    // Return parent agents (those without parent_agent_id) with their children
    return filteredAgents
      .filter(agent => !agent.parent_agent_id) // Only top-level agents
      .map(parent => ({
        parent,
        children: childrenByParent.get(parent.id) || [],
      }));
  }, [filteredAgents]);

  const handleAgentArchiveToggle = (agentId: string, archived: boolean) => {
    updateAgent(agentId, { archived });
  };

  const toggleFilter = (filter: StatusFilter) => {
    setStatusFilter(current => current === filter ? 'all' : filter);
  };
  const hasSelectedAgent = selectedAgentId && agents[selectedAgentId];

  // Transform merge queue data to component format
  const mergeQueueData = useMemo(() => {
    if (!mergeQueue) {
      return {
        completed: [],
        resolvers: [],
        pending: [],
        activeWorkers: [],
      };
    }

    return {
      completed: mergeQueue.completed.map((item) => ({
        taskId: item.task_id,
        agentId: item.agent_id,
        timestamp: item.timestamp,
        success: item.success,
        ...(item.error !== undefined && { error: item.error }),
      })),
      resolvers: mergeQueue.resolvers.map((item) => ({
        parentTaskId: item.parent_task_id,
        resolverTaskId: item.resolver_task_id,
        parentAgentId: item.parent_agent_id,
        resolverAgentId: item.resolver_agent_id,
        status: item.status === 'completed' ? 'resolved' as const : item.status === 'failed' ? 'failed' as const : 'resolving' as const,
      })),
      pending: mergeQueue.pending.map((item) => ({
        taskId: item.task_id,
        agentId: item.agent_id,
        position: item.position,
      })),
      activeWorkers: mergeQueue.active_workers.map((item) => ({
        taskId: item.task_id,
        agentId: item.agent_id,
        status: item.status as 'pending' | 'acquiring' | 'merging' | 'resolving' | 'merged' | 'failed',
      })),
    };
  }, [mergeQueue]);

  // Check if merge queue has any activity
  const hasMergeQueueActivity = mergeQueueData.completed.length > 0 ||
    mergeQueueData.resolvers.length > 0 ||
    mergeQueueData.pending.length > 0 ||
    mergeQueueData.activeWorkers.length > 0;

  // Get node details from agent state for tooltips
  const getNodeDetails = useCallback((taskId: string): NodeDetails | null => {
    const agent = Object.values(agents).find(a => a.task_id === taskId);
    if (!agent) return null;

    return {
      title: agent.task_title,
      duration: agent.duration,
      tokenUsage: agent.token_usage,
      filesChanged: agent.changes,
      commitCount: agent.commits,
      commits: agent.git_commits,
      output: agent.output,
    };
  }, [agents]);

  const formatCost = (cost: number): string => {
    if (cost < 0.01) {
      return `$${(cost * 100).toFixed(2)}c`;
    }
    return `$${cost.toFixed(2)}`;
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
          {/* Title, Repository Selector, and Connection Status */}
          <div className="flex items-center gap-4">
            <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">Canopy Dashboard</h1>
            <RepoSelector
              repositories={repositories}
              activeRepoId={activeRepoId}
              onSelect={handleRepoSelect}
              isLoading={isRepoSwitching}
              disabled={!connected}
            />
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
          </div>

          {/* Pause/Resume Button */}
          <div className="flex items-center gap-4">
            {/* Stats Summary (non-interactive) */}
            <div className="flex items-center gap-4 text-sm">
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Zap className="w-4 h-4" />
                <span className="font-medium">{formatTokens(stats.total_tokens)}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <DollarSign className="w-4 h-4" />
                <span className="font-medium">{formatCost(stats.total_cost_usd)}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <FileEdit className="w-4 h-4" />
                <span className="font-medium">{stats.file_changes}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <GitCommit className="w-4 h-4" />
                <span className="font-medium">{stats.git_commits}</span>
              </div>
            </div>

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

        {/* Stats Filter Toggles */}
        <div className="flex items-center gap-3 mt-4">
          {/* All Tasks */}
          <button
            onClick={() => setStatusFilter('all')}
            className={`
              flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all cursor-pointer
              ${statusFilter === 'all'
                ? 'bg-gray-200 dark:bg-gray-600 ring-2 ring-gray-400 dark:ring-gray-500'
                : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
              }
            `}
          >
            <ListTodo className="w-4 h-4 text-gray-600 dark:text-gray-400" />
            <span className="text-sm text-gray-600 dark:text-gray-300">All</span>
            <span className="text-lg font-bold text-gray-900 dark:text-gray-100">{stats.total_tasks}</span>
          </button>

          {/* Running */}
          <button
            onClick={() => toggleFilter('running')}
            className={`
              flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all cursor-pointer
              ${statusFilter === 'running'
                ? 'bg-blue-100 dark:bg-blue-900/50 ring-2 ring-blue-500'
                : 'bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50'
              }
            `}
          >
            <Activity className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            <span className="text-sm text-blue-600 dark:text-blue-400">Running</span>
            <span className="text-lg font-bold text-blue-700 dark:text-blue-300">{stats.running_tasks}</span>
          </button>

          {/* Completed */}
          <button
            onClick={() => toggleFilter('completed')}
            className={`
              flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all cursor-pointer
              ${statusFilter === 'completed'
                ? 'bg-green-100 dark:bg-green-900/50 ring-2 ring-green-500'
                : 'bg-green-50 dark:bg-green-900/30 hover:bg-green-100 dark:hover:bg-green-900/50'
              }
            `}
          >
            <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400" />
            <span className="text-sm text-green-600 dark:text-green-400">Completed</span>
            <span className="text-lg font-bold text-green-700 dark:text-green-300">{stats.completed_tasks}</span>
          </button>

          {/* Failed */}
          <button
            onClick={() => toggleFilter('failed')}
            className={`
              flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all cursor-pointer
              ${statusFilter === 'failed'
                ? 'bg-red-100 dark:bg-red-900/50 ring-2 ring-red-500'
                : 'bg-red-50 dark:bg-red-900/30 hover:bg-red-100 dark:hover:bg-red-900/50'
              }
            `}
          >
            <XCircle className="w-4 h-4 text-red-600 dark:text-red-400" />
            <span className="text-sm text-red-600 dark:text-red-400">Failed</span>
            <span className="text-lg font-bold text-red-700 dark:text-red-300">{stats.failed_tasks}</span>
          </button>

          {/* Spacer */}
          <div className="flex-1" />

          {/* Show Archived Toggle */}
          <button
            onClick={() => setShowArchivedAgents(!showArchivedAgents)}
            className={`
              flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all cursor-pointer
              ${showArchivedAgents
                ? 'bg-purple-100 dark:bg-purple-900/50 ring-2 ring-purple-500'
                : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
              }
            `}
            title={showArchivedAgents ? 'Hide archived agents' : 'Show archived agents'}
          >
            <Archive className={`w-4 h-4 ${showArchivedAgents ? 'text-purple-600 dark:text-purple-400' : 'text-gray-600 dark:text-gray-400'}`} />
            <span className={`text-sm ${showArchivedAgents ? 'text-purple-600 dark:text-purple-400' : 'text-gray-600 dark:text-gray-400'}`}>
              {showArchivedAgents ? 'Hide' : 'Show'} Archived
            </span>
            <span className={`text-lg font-bold ${showArchivedAgents ? 'text-purple-700 dark:text-purple-300' : 'text-gray-700 dark:text-gray-300'}`}>
              {archivedAgentCount}
            </span>
          </button>
        </div>
      </header>

      {/* Main Content Area with Beads Pane */}
      <div className="flex-1 flex overflow-hidden">
        {/* Beads Left Pane */}
        <BeadsPane
          isExpanded={beadsPaneExpanded}
          onToggle={() => setBeadsPaneExpanded(!beadsPaneExpanded)}
        />

        {/* Main Content */}
        <main className="flex-1 flex flex-col overflow-hidden">
          {/* Merge Queue Panel - Collapsible */}
          {hasMergeQueueActivity && (
            <div className="flex-shrink-0 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
              <button
                onClick={() => setMergeQueueExpanded(!mergeQueueExpanded)}
                className="w-full flex items-center justify-between px-4 py-2 hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors"
              >
                <div className="flex items-center gap-2">
                  <GitMerge className="w-4 h-4 text-amber-500" />
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    Merge Queue
                  </span>
                  {mergeQueueData.resolvers.length > 0 && (
                    <span className="px-2 py-0.5 text-xs font-medium bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300 rounded-full animate-pulse">
                      {mergeQueueData.resolvers.length} resolving
                    </span>
                  )}
                  <span className="text-xs text-gray-500 dark:text-gray-400">
                    ({mergeQueueData.completed.length} completed, {mergeQueueData.activeWorkers.length} active, {mergeQueueData.pending.length} pending)
                  </span>
                </div>
                {mergeQueueExpanded ? (
                  <ChevronUp className="w-4 h-4 text-gray-500" />
                ) : (
                  <ChevronDown className="w-4 h-4 text-gray-500" />
                )}
              </button>
              {mergeQueueExpanded && (
                <div className="px-4 pb-4">
                  <MergeQueueTree
                    completed={mergeQueueData.completed}
                    resolvers={mergeQueueData.resolvers}
                    pending={mergeQueueData.pending}
                    activeWorkers={mergeQueueData.activeWorkers}
                    getNodeDetails={getNodeDetails}
                    onNodeClick={(taskId) => {
                      // Find agent by task ID and select it
                      const agent = Object.values(agents).find(a => a.task_id === taskId);
                      if (agent) {
                        handleSelectAgent(agent.id);
                      }
                    }}
                  />
                </div>
              )}
            </div>
          )}

          {/* Agent Grid */}
          <div className="flex-1 overflow-y-auto p-6">
          {groupedAgents.length === 0 ? (
            <div className="flex items-center justify-center h-full">
              <div className="text-center">
                <Activity className="w-16 h-16 text-gray-300 dark:text-gray-600 mx-auto mb-4" />
                <h3 className="text-lg font-medium text-gray-500 dark:text-gray-400 mb-2">
                  {agentList.length === 0 ? 'No Agents' : `No ${statusFilter === 'all' ? '' : statusFilter} Agents`}
                </h3>
                <p className="text-sm text-gray-400 dark:text-gray-500">
                  {agentList.length === 0
                    ? 'Agents will appear here when tasks are running'
                    : 'Try selecting a different filter above'
                  }
                </p>
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 items-start">
              {groupedAgents.map(({ parent, children }) => (
                <AgentCardGroup
                  key={parent.id}
                  parentAgent={parent}
                  childAgents={children}
                  onSelect={handleSelectAgent}
                  selectedAgentId={selectedAgentId}
                  onArchiveToggle={handleAgentArchiveToggle}
                />
              ))}
            </div>
          )}
        </div>

          {/* Bottom Terminal Panel */}
          {hasSelectedAgent && (
            <div
              className="border-t border-gray-200 dark:border-gray-700 bg-gray-900 flex-shrink-0"
              style={{ height: paneHeight }}
            >
              <div className="h-full flex flex-col">
                {/* Resize Handle */}
                <div
                  onMouseDown={handleResizeStart}
                  className={`
                    h-1.5 bg-gray-800 cursor-ns-resize flex items-center justify-center
                    hover:bg-gray-700 transition-colors group
                    ${isResizing ? 'bg-blue-600' : ''}
                  `}
                  title="Drag to resize"
                >
                  <GripHorizontal className={`w-4 h-4 text-gray-600 group-hover:text-gray-400 ${isResizing ? 'text-blue-400' : ''}`} />
                </div>

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
                      <button
                        onClick={() => setActiveTab('commits')}
                        className={`
                          flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                          ${activeTab === 'commits'
                            ? 'bg-blue-600 text-white'
                            : 'text-gray-400 hover:text-gray-200 hover:bg-gray-700/50'
                          }
                        `}
                      >
                        <GitCommit className="w-4 h-4" />
                        Commits
                        {(agents[selectedAgentId]?.commits ?? 0) > 0 && (
                          <span className="ml-1 px-1.5 py-0.5 text-xs bg-blue-500/30 rounded">
                            {agents[selectedAgentId]?.commits}
                          </span>
                        )}
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
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => setSelectedAgent(null)}
                      className="text-gray-400 hover:text-gray-200 transition-colors px-2 py-1 rounded hover:bg-gray-700"
                    >
                      <span className="text-sm">Close</span>
                    </button>
                  </div>
                </div>

                {/* Tab Content */}
                <div className="flex-1 overflow-hidden">
                  {activeTab === 'feed' && (
                    <LiveFeed agentId={selectedAgentId} />
                  )}
                  {activeTab === 'terminal' && (
                    <AgentTerminal agentId={selectedAgentId} />
                  )}
                  {activeTab === 'commits' && (
                    <div className="h-full overflow-y-auto p-4 bg-gray-900">
                      <CommitList commits={agents[selectedAgentId]?.git_commits || []} />
                    </div>
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
