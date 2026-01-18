import React, { useEffect, useState, useCallback } from 'react';
import { useStateStore } from '../../stores/stateStore';
import { useWebSocket, useAgentFiltering, useResizablePane, useResizableWidth } from '../../hooks';
import type { StatusFilter } from '../../hooks';
import { pauseOrch, resumeOrch, getState, getRepositories, activateRepository, getRuns } from '../../api/client';
import { BeadsPane } from '../beads/BeadsPane';
import { DashboardHeader } from './DashboardHeader';
import { TerminalPanel } from './TerminalPanel';
import { AgentGrid } from '../agents/AgentGrid';

const SHOW_ARCHIVED_AGENTS_KEY = 'canopy-show-archived-agents';
const SHOW_COMPLETED_BEADS_KEY = 'canopy-show-completed-beads';

export const Dashboard: React.FC = () => {
  const { connected } = useWebSocket();

  // Orchestrator control state
  const [isPauseLoading, setIsPauseLoading] = useState(false);
  const [isResumeLoading, setIsResumeLoading] = useState(false);

  // Theme state
  const [isDark, setIsDark] = useState(() => {
    const saved = localStorage.getItem('darkMode');
    if (saved !== null) return saved === 'true';
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  });

  // Filter state
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [showArchivedAgents, setShowArchivedAgents] = useState(() => {
    const saved = localStorage.getItem(SHOW_ARCHIVED_AGENTS_KEY);
    return saved === 'true';
  });

  // Beads pane state
  const [beadsPaneExpanded, setBeadsPaneExpanded] = useState(() => {
    const saved = localStorage.getItem('beadsPaneExpanded');
    return saved !== null ? saved === 'true' : true;
  });
  const [showCompletedBeads, setShowCompletedBeads] = useState(() => {
    const saved = localStorage.getItem(SHOW_COMPLETED_BEADS_KEY);
    return saved === 'true';
  });

  // Resizable terminal pane
  const { height: paneHeight, isResizing, handleResizeStart } = useResizablePane({
    storageKey: 'outputPaneHeight',
    defaultHeight: 320,
    minHeight: 200,
    maxHeightRatio: 0.8,
  });

  // Resizable beads pane width
  const { width: beadsPaneWidth, isResizing: isBeadsResizing, handleResizeStart: handleBeadsResizeStart } = useResizableWidth({
    storageKey: 'beadsPaneWidth',
    defaultWidth: 320,
    minWidth: 200,
    maxWidthRatio: 0.5,
  });

  // Persist beads pane state
  useEffect(() => {
    localStorage.setItem('beadsPaneExpanded', String(beadsPaneExpanded));
  }, [beadsPaneExpanded]);

  // Persist show completed beads state
  useEffect(() => {
    localStorage.setItem(SHOW_COMPLETED_BEADS_KEY, String(showCompletedBeads));
  }, [showCompletedBeads]);

  // Auto-switch to 'all' agents mode when showing completed beads
  useEffect(() => {
    if (showCompletedBeads) {
      setStatusFilter('all');
    }
  }, [showCompletedBeads]);

  // Persist show archived agents state
  useEffect(() => {
    localStorage.setItem(SHOW_ARCHIVED_AGENTS_KEY, String(showArchivedAgents));
  }, [showArchivedAgents]);

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
  const isPausedByResolver = useStateStore((state) => state.isPausedByResolver);
  const pauseState = useStateStore((state) => state.pauseState);
  const selectedAgentId = useStateStore((state) => state.selectedAgentId);
  const setSelectedAgent = useStateStore((state) => state.setSelectedAgent);
  const syncState = useStateStore((state) => state.syncState);
  const updateAgent = useStateStore((state) => state.updateAgent);
  const repositories = useStateStore((state) => state.repositories);
  const activeRepoId = useStateStore((state) => state.activeRepoId);
  const isRepoSwitching = useStateStore((state) => state.isRepoSwitching);
  const setRepositories = useStateStore((state) => state.setRepositories);
  const setActiveRepo = useStateStore((state) => state.setActiveRepo);
  const setRepoSwitching = useStateStore((state) => state.setRepoSwitching);
  const runs = useStateStore((state) => state.runs);
  const activeRunId = useStateStore((state) => state.activeRunId);
  const isRunsLoading = useStateStore((state) => state.isRunsLoading);
  const setRuns = useStateStore((state) => state.setRuns);
  const setActiveRunId = useStateStore((state) => state.setActiveRunId);
  const setRunsLoading = useStateStore((state) => state.setRunsLoading);
  const selectedBeadId = useStateStore((state) => state.selectedBeadId);
  const setSelectedBead = useStateStore((state) => state.setSelectedBead);
  const tasks = useStateStore((state) => state.tasks);

  // Use the agent filtering hook
  const { groupedAgents, archivedCount } = useAgentFiltering({
    agents,
    statusFilter,
    showArchived: showArchivedAgents,
    activeRunId,
    selectedBeadId,
  });

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
          }
        }

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

  // Load runs when activeRepoId changes
  useEffect(() => {
    const loadRuns = async () => {
      if (!activeRepoId) return;

      try {
        setRunsLoading(true);
        const response = await getRuns({ repo_id: activeRepoId, limit: 50 });
        setRuns(response.runs);
        setActiveRunId('');
      } catch (error) {
        console.error('Failed to load runs:', error);
        setRuns([]);
      } finally {
        setRunsLoading(false);
      }
    };

    loadRuns();
  }, [activeRepoId, setRuns, setActiveRunId, setRunsLoading]);

  // Event handlers
  const handlePause = async () => {
    if (isPauseLoading) return;

    try {
      setIsPauseLoading(true);
      await pauseOrch();
      // State update comes from WebSocket event
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
      // State update comes from WebSocket event
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
      setActiveRunId('');
      const state = await getState();
      syncState(state);
    } catch (error) {
      console.error('Failed to switch repository:', error);
    } finally {
      setRepoSwitching(false);
    }
  };

  const handleSelectAgent = useCallback((agentId: string) => {
    setSelectedAgent(agentId);
  }, [setSelectedAgent]);

  const handleAgentArchiveToggle = useCallback((agentId: string, archived: boolean) => {
    updateAgent(agentId, { archived });
  }, [updateAgent]);

  // Handle task click from BeadsPane merge queue status
  const handleMergeTaskClick = useCallback((taskId: string) => {
    const agent = Object.values(agents).find(a => a.task_id === taskId);
    if (agent) {
      handleSelectAgent(agent.id);
    }
  }, [agents, handleSelectAgent]);

  const selectedAgent = selectedAgentId ? agents[selectedAgentId] : null;
  const totalAgentCount = Object.keys(agents).length;

  return (
    <div className="flex flex-col h-screen bg-gray-50 dark:bg-gray-900">
      <DashboardHeader
        connected={connected}
        isPaused={isPaused}
        isPausedByResolver={isPausedByResolver}
        pauseState={pauseState}
        isPauseLoading={isPauseLoading}
        isResumeLoading={isResumeLoading}
        isDark={isDark}
        onToggleTheme={() => setIsDark(!isDark)}
        repositories={repositories}
        activeRepoId={activeRepoId}
        isRepoSwitching={isRepoSwitching}
        onRepoSelect={handleRepoSelect}
        runs={runs}
        activeRunId={activeRunId}
        isRunsLoading={isRunsLoading}
        onRunSelect={setActiveRunId}
        stats={stats}
        onPause={handlePause}
        onResume={handleResume}
      />

      {/* Main Content Area with Beads Pane */}
      <div className="flex-1 flex overflow-hidden">
        {/* Beads Left Pane */}
        <BeadsPane
          isExpanded={beadsPaneExpanded}
          onToggle={() => setBeadsPaneExpanded(!beadsPaneExpanded)}
          onTaskClick={handleMergeTaskClick}
          showCompleted={showCompletedBeads}
          onToggleShowCompleted={() => setShowCompletedBeads(!showCompletedBeads)}
          width={beadsPaneWidth}
          isResizing={isBeadsResizing}
          onResizeStart={handleBeadsResizeStart}
          selectedBeadId={selectedBeadId}
          onBeadSelect={setSelectedBead}
        />

        {/* Main Content */}
        <main className="flex-1 flex flex-col overflow-hidden">
          {/* Agent Grid */}
          <div className="flex-1 overflow-y-auto p-8">
            <AgentGrid
              groupedAgents={groupedAgents}
              totalAgentCount={totalAgentCount}
              statusFilter={statusFilter}
              onStatusFilterChange={setStatusFilter}
              stats={stats}
              showArchivedAgents={showArchivedAgents}
              archivedAgentCount={archivedCount}
              onToggleShowArchived={() => setShowArchivedAgents(!showArchivedAgents)}
              selectedAgentId={selectedAgentId}
              onSelectAgent={handleSelectAgent}
              onArchiveToggle={handleAgentArchiveToggle}
              selectedBeadId={selectedBeadId}
              selectedBeadTitle={selectedBeadId ? tasks[selectedBeadId]?.title : undefined}
              onClearBeadFilter={() => setSelectedBead(null)}
            />
          </div>

          {/* Bottom Terminal Panel */}
          {selectedAgent && (
            <TerminalPanel
              agent={selectedAgent}
              height={paneHeight}
              isResizing={isResizing}
              onResizeStart={handleResizeStart}
              onClose={() => setSelectedAgent(null)}
            />
          )}
        </main>
      </div>
    </div>
  );
};
