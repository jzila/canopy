import { useState, useCallback, useEffect } from 'react';
import { useStateStore } from '../stores/stateStore';
import {
  getAgentConfig,
  updateAgentConfig,
  persistAgentConfig,
  type AgentConfigResponse,
  type AgentConfigUpdateRequest,
} from '../api/client';

export interface UseAgentConfigResult {
  config: AgentConfigResponse | null;
  isLoading: boolean;
  isSaving: boolean;
  isPersisting: boolean;
  error: string | null;
  reload: () => Promise<void>;
  update: (request: AgentConfigUpdateRequest) => Promise<boolean>;
  persist: () => Promise<boolean>;
}

export function useAgentConfig(): UseAgentConfigResult {
  const [config, setConfig] = useState<AgentConfigResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isPersisting, setIsPersisting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const repositories = useStateStore((state) => state.repositories);
  const activeRepoId = useStateStore((state) => state.activeRepoId);

  const activeRepo = repositories.find((r) => r.id === activeRepoId);
  const repoPath = activeRepo?.path ?? '';

  const reload = useCallback(async () => {
    if (!repoPath) {
      setError('No active repository selected');
      return;
    }
    try {
      setIsLoading(true);
      setError(null);
      const response = await getAgentConfig(repoPath);
      if (response.error) {
        setError(response.error);
      } else {
        setConfig(response);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load agent config');
    } finally {
      setIsLoading(false);
    }
  }, [repoPath]);

  const update = useCallback(async (request: AgentConfigUpdateRequest): Promise<boolean> => {
    if (!repoPath) {
      setError('No active repository selected');
      return false;
    }
    try {
      setIsSaving(true);
      setError(null);
      const response = await updateAgentConfig(repoPath, request);
      if (response.success && response.settings) {
        setConfig(response.settings);
        return true;
      }
      if (response.error) {
        setError(response.error);
      }
      return false;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update agent config');
      return false;
    } finally {
      setIsSaving(false);
    }
  }, [repoPath]);

  const persist = useCallback(async (): Promise<boolean> => {
    if (!repoPath) {
      setError('No active repository selected');
      return false;
    }
    try {
      setIsPersisting(true);
      setError(null);
      const response = await persistAgentConfig(repoPath);
      if (response.success) {
        // Reload to get updated persisted state
        await reload();
        return true;
      }
      if (response.error) {
        setError(response.error);
      }
      return false;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to persist agent config');
      return false;
    } finally {
      setIsPersisting(false);
    }
  }, [repoPath, reload]);

  // Reload when repo changes
  useEffect(() => {
    if (repoPath) {
      reload();
    }
  }, [repoPath, reload]);

  // Listen for WebSocket agent_config:changed events
  useEffect(() => {
    const handler = () => {
      reload();
    };
    window.addEventListener('agent_config:changed', handler);
    return () => window.removeEventListener('agent_config:changed', handler);
  }, [reload]);

  return { config, isLoading, isSaving, isPersisting, error, reload, update, persist };
}
