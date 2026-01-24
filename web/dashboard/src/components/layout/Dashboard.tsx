import React, { useEffect, useState, useCallback } from 'react';
import { useStateStore } from '../../stores/stateStore';
import type { RunConfig } from '../../stores/stateStore';
import { useWebSocket, useAgentFiltering, useResizablePane, useResizableWidth } from '../../hooks';
import type { StatusFilter } from '../../hooks';
import { pauseOrch, resumeOrch, getState, getRepositories, activateRepository, getRuns, startRun, stopRun, saveRunConfig } from '../../api/client';
import { BeadsPane } from '../beads/BeadsPane';
import { DashboardHeader } from './DashboardHeader';
import { TerminalPanel } from './TerminalPanel';
import { AgentGrid } from '../agents/AgentGrid';
import { RunConfigDialog } from '../runs/RunConfigDialog';
import { RulesPanel } from '../rules/RulesPanel';

const SHOW_ARCHIVED_AGENTS_KEY = 'canopy-show-archived-agents';
const SHOW_COMPLETED_BEADS_KEY = 'canopy-show-completed-beads';

export const Dashboard: React.FC = () => {
  // Initialize WebSocket connection
  useWebSocket();
  const connected = useStateStore((state) => state.connected);

  // Orchestrator control state
  const [isPauseLoading, setIsPauseLoading] = useState(false);
  const [isResumeLoading, setIsResumeLoading] = useState(false);
  const [isSavingConfig, setIsSavingConfig] = useState(false);
  const [configDialogMode, setConfigDialogMode] = useState<'start' | 'configure'>('start');

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

  // Rules pane state
  const [rulesPaneExpanded, setRulesPaneExpanded] = useState(() => {
    const saved = localStorage.getItem('rulesPaneExpanded');
    return saved !== null ? saved === 'true' : false;
  });

  // Resizable rules pane width
  const { width: rulesPaneWidth, isResizing: isRulesResizing, handleResizeStart: handleRulesResizeStart } = useResizableWidth({
    storageKey: 'rulesPaneWidth',
    defaultWidth: 280,
    minWidth: 200,
    maxWidthRatio: 0.4,
  });

  // Persist beads pane state
  useEffect(() => {
    localStorage.setItem('beadsPaneExpanded', String(beadsPaneExpanded));
  }, [beadsPaneExpanded]);

  // Persist rules pane state
  useEffect(() => {
    localStorage.setItem('rulesPaneExpanded', String(rulesPaneExpanded));
  }, [rulesPaneExpanded]);

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
  const mergeRuns = useStateStore((state) => state.mergeRuns);
  const setActiveRunId = useStateStore((state) => state.setActiveRunId);
  const setRunsLoading = useStateStore((state) => state.setRunsLoading);
  const selectedBeadId = useStateStore((state) => state.selectedBeadId);
  const setSelectedBead = useStateStore((state) => state.setSelectedBead);
  const tasks = useStateStore((state) => state.tasks);
  // Orchestrator state
  const orchestratorState = useStateStore((state) => state.orchestratorState);
  const activeAgentCount = useStateStore((state) => state.activeAgentCount);
  // Run control state
  const isStartingRun = useStateStore((state) => state.isStartingRun);
  const isStoppingRun = useStateStore((state) => state.isStoppingRun);
  const runConfig = useStateStore((state) => state.runConfig);
  const showRunConfigDialog = useStateStore((state) => state.showRunConfigDialog);
  const setShowRunConfigDialog = useStateStore((state) => state.setShowRunConfigDialog);
  const setRunConfig = useStateStore((state) => state.setRunConfig);
  const loadSavedConfig = useStateStore((state) => state.loadSavedConfig);
  // Optimistic update actions
  const startOptimisticRun = useStateStore((state) => state.startOptimisticRun);
  const confirmRunStarted = useStateStore((state) => state.confirmRunStarted);
  const rollbackRunStart = useStateStore((state) => state.rollbackRunStart);
  const startOptimisticStop = useStateStore((state) => state.startOptimisticStop);
  const confirmRunStopped = useStateStore((state) => state.confirmRunStopped);
  const rollbackRunStop = useStateStore((state) => state.rollbackRunStop);
  const startOptimisticPause = useStateStore((state) => state.startOptimisticPause);
  const rollbackPause = useStateStore((state) => state.rollbackPause);
  const startOptimisticResume = useStateStore((state) => state.startOptimisticResume);
  const rollbackResume = useStateStore((state) => state.rollbackResume);

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

        // Load saved run configuration
        await loadSavedConfig();
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
  }, [syncState, setRepositories, loadSavedConfig]);

  // Load runs when activeRepoId changes
  useEffect(() => {
    const loadRuns = async () => {
      if (!activeRepoId) return;

      try {
        setRunsLoading(true);
        const response = await getRuns({ repo_id: activeRepoId, limit: 50 });
        // Use mergeRuns instead of setRuns to avoid race condition where
        // a WebSocket run:started event adds a run before this API call completes.
        // mergeRuns preserves any runs added via WebSocket that aren't yet in the API response.
        mergeRuns(response.runs);
        setActiveRunId('');
      } catch (error) {
        console.error('Failed to load runs:', error);
        setRuns([]);
      } finally {
        setRunsLoading(false);
      }
    };

    loadRuns();
  }, [activeRepoId, mergeRuns, setRuns, setActiveRunId, setRunsLoading]);

  // Event handlers - using optimistic updates
  const handlePause = async () => {
    if (isPauseLoading) return;

    // Optimistic update - immediately show paused state
    startOptimisticPause();
    setIsPauseLoading(true);

    try {
      await pauseOrch();
      // WebSocket event will confirm the pause with final state
    } catch (error) {
      console.error('Failed to pause orchestration:', error);
      // Rollback to previous state on error
      rollbackPause(error instanceof Error ? error.message : 'Failed to pause');
    } finally {
      setIsPauseLoading(false);
    }
  };

  const handleResume = async () => {
    if (isResumeLoading) return;

    // Optimistic update - immediately show resumed state
    startOptimisticResume();
    setIsResumeLoading(true);

    try {
      await resumeOrch();
      // WebSocket event will confirm the resume with final state
    } catch (error) {
      console.error('Failed to resume orchestration:', error);
      // Rollback to previous state on error
      rollbackResume(error instanceof Error ? error.message : 'Failed to resume');
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

  // Run control handlers
  const handleCloseRunConfig = useCallback(() => {
    setShowRunConfigDialog(false);
    setConfigDialogMode('start');
  }, [setShowRunConfigDialog]);

  const handleConfigure = useCallback(() => {
    setConfigDialogMode('configure');
    setShowRunConfigDialog(true);
  }, [setShowRunConfigDialog]);

  const handleSaveConfig = useCallback(async (config: RunConfig) => {
    setIsSavingConfig(true);
    try {
      const response = await saveRunConfig({
        concurrency: config.concurrency,
        max_priority: config.max_priority,
        use_bwrap: config.use_bwrap,
        max_retries: config.max_retries,
      });

      if (!response.error) {
        // Update local state (WebSocket event will also update it for cross-tab sync)
        setRunConfig(config);
        setShowRunConfigDialog(false);
        setConfigDialogMode('start');
      } else {
        console.error('Failed to save config:', response.error);
      }
    } catch (error) {
      console.error('Failed to save config:', error);
    } finally {
      setIsSavingConfig(false);
    }
  }, [setRunConfig, setShowRunConfigDialog]);

  const handleStartRun = useCallback(async (config: RunConfig) => {
    const activeRepo = repositories.find((repo) => repo.id === activeRepoId);
    if (!activeRepo) {
      console.error('No active repository selected');
      return;
    }

    // Generate a temporary run ID for optimistic update
    const tempRunId = `temp-${Date.now()}`;

    // Optimistic update - immediately show starting state
    startOptimisticRun(tempRunId);

    try {
      const response = await startRun({
        work_dir: activeRepo.path,
        repo_id: activeRepo.id,
        concurrency: config.concurrency,
        max_priority: config.max_priority,
        use_bwrap: config.use_bwrap,
        max_retries: config.max_retries,
      });

      if (response.success && response.run_id) {
        // Confirm with the actual run ID
        confirmRunStarted(response.run_id);
        setShowRunConfigDialog(false);
        // Refresh state after starting run
        const state = await getState();
        syncState(state);
      } else {
        // Rollback on failure response
        rollbackRunStart(response.error || 'Failed to start run');
      }
    } catch (error) {
      console.error('Failed to start run:', error);
      // Rollback on exception
      rollbackRunStart(error instanceof Error ? error.message : 'Failed to start run');
    }
  }, [activeRepoId, repositories, startOptimisticRun, confirmRunStarted, rollbackRunStart, setShowRunConfigDialog, syncState]);

  const handleStopRun = useCallback(async () => {
    const runId = useStateStore.getState().currentRunId;
    if (!runId) {
      console.error('No active run to stop');
      return;
    }

    // Optimistic update - immediately show stopping state
    startOptimisticStop();

    try {
      const response = await stopRun(runId);

      if (response.success) {
        // Confirm the stop
        confirmRunStopped();
        // Refresh state after stopping run
        const state = await getState();
        syncState(state);
      } else {
        // Rollback on failure response
        rollbackRunStop(response.error || 'Failed to stop run');
      }
    } catch (error) {
      console.error('Failed to stop run:', error);
      // Rollback on exception
      rollbackRunStop(error instanceof Error ? error.message : 'Failed to stop run');
    }
  }, [startOptimisticStop, confirmRunStopped, rollbackRunStop, syncState]);

  // Activate orchestrator - starts with current run config
  const handleActivate = useCallback(async () => {
    const activeRepo = repositories.find((repo) => repo.id === activeRepoId);
    if (!activeRepo) {
      console.error('No active repository selected');
      return;
    }

    // Generate a temporary run ID for optimistic update
    const tempRunId = `temp-${Date.now()}`;

    // Optimistic update - immediately show activating state
    startOptimisticRun(tempRunId);

    try {
      const config = useStateStore.getState().runConfig;
      const response = await startRun({
        work_dir: activeRepo.path,
        repo_id: activeRepo.id,
        concurrency: config.concurrency,
        max_priority: config.max_priority,
        use_bwrap: config.use_bwrap,
        max_retries: config.max_retries,
      });

      if (response.success && response.run_id) {
        // Confirm with the actual run ID
        confirmRunStarted(response.run_id);
        // Refresh state after activating
        const state = await getState();
        syncState(state);
      } else {
        // Rollback on failure response
        rollbackRunStart(response.error || 'Failed to activate orchestrator');
      }
    } catch (error) {
      console.error('Failed to activate orchestrator:', error);
      // Rollback on exception
      rollbackRunStart(error instanceof Error ? error.message : 'Failed to activate orchestrator');
    }
  }, [activeRepoId, repositories, startOptimisticRun, confirmRunStarted, rollbackRunStart, syncState]);

  // Deactivate orchestrator - same as stop run
  const handleDeactivate = handleStopRun;

  const selectedAgent = selectedAgentId ? agents[selectedAgentId] : null;
  const totalAgentCount = Object.keys(agents).length;
  const currentRunId = useStateStore((state) => state.currentRunId);

  return (
    <div className="flex flex-col h-screen bg-gray-50 dark:bg-gray-900">
      <DashboardHeader
        connected={connected}
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
        orchestratorState={orchestratorState}
        activeAgentCount={activeAgentCount}
        pauseState={pauseState}
        isActivating={isStartingRun}
        isDeactivating={isStoppingRun}
        isPauseLoading={isPauseLoading}
        isResumeLoading={isResumeLoading}
        onActivate={handleActivate}
        onDeactivate={handleDeactivate}
        onPause={handlePause}
        onResume={handleResume}
        onConfigure={handleConfigure}
        stats={stats}
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
          <div className="flex-1 flex overflow-hidden">
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

            {/* Rules Panel (Right Side) */}
            <RulesPanel
              isExpanded={rulesPaneExpanded}
              onToggle={() => setRulesPaneExpanded(!rulesPaneExpanded)}
              width={rulesPaneWidth}
              isResizing={isRulesResizing}
              onResizeStart={handleRulesResizeStart}
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

      {/* Run Configuration Dialog */}
      <RunConfigDialog
        isOpen={showRunConfigDialog}
        onClose={handleCloseRunConfig}
        onStart={handleStartRun}
        onSave={handleSaveConfig}
        isStarting={isStartingRun}
        isSaving={isSavingConfig}
        initialConfig={runConfig}
        repoName={repositories.find((r) => r.id === activeRepoId)?.name}
        mode={configDialogMode}
      />
    </div>
  );
};
