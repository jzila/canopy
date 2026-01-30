import React, { useState, useEffect, useRef } from 'react';
import { X, Settings, Save, RefreshCw, Loader2, AlertTriangle, Bot, Check } from 'lucide-react';
import { useAgentConfig } from '../../hooks/useAgentConfig';
import type { AgentConfigUpdateRequest, AgentTypeSettings } from '../../api/client';

// Known model IDs for the dropdown
const KNOWN_MODELS = [
  '',
  'claude-sonnet-4-20250514',
  'claude-opus-4-20250514',
  'claude-haiku-3-5-20241022',
];

interface AgentConfigPanelProps {
  isOpen: boolean;
  onClose: () => void;
}

interface AgentTypeFormState {
  model: string;
  useDefault: boolean;
  enabled: boolean;
  timeout: string;
}

function settingsToFormState(settings: AgentTypeSettings, defaultModel: string): AgentTypeFormState {
  return {
    model: settings.Model || '',
    useDefault: !settings.Model,
    enabled: settings.Enabled !== false,
    timeout: settings.Timeout || '',
  };
}

export const AgentConfigPanel: React.FC<AgentConfigPanelProps> = ({ isOpen, onClose }) => {
  const { config, isLoading, isSaving, isPersisting, error, reload, update, persist } = useAgentConfig();
  const dialogRef = useRef<HTMLDivElement>(null);

  const [defaultModel, setDefaultModel] = useState('');
  const [customDefaultModel, setCustomDefaultModel] = useState('');
  const [worker, setWorker] = useState<AgentTypeFormState>({ model: '', useDefault: true, enabled: true, timeout: '' });
  const [resolver, setResolver] = useState<AgentTypeFormState>({ model: '', useDefault: true, enabled: true, timeout: '' });
  const [repair, setRepair] = useState<AgentTypeFormState>({ model: '', useDefault: true, enabled: true, timeout: '' });
  const [hasChanges, setHasChanges] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);

  // Sync form state from config
  useEffect(() => {
    if (config) {
      const dm = config.default_model || '';
      setDefaultModel(KNOWN_MODELS.includes(dm) ? dm : 'custom');
      setCustomDefaultModel(KNOWN_MODELS.includes(dm) ? '' : dm);
      setWorker(settingsToFormState(config.worker, dm));
      setResolver(settingsToFormState(config.resolver, dm));
      setRepair(settingsToFormState(config.repair, dm));
      setHasChanges(false);
    }
  }, [config]);

  // Track changes
  useEffect(() => {
    if (!config) return;
    const effectiveDefault = defaultModel === 'custom' ? customDefaultModel : defaultModel;
    const changed =
      effectiveDefault !== (config.default_model || '') ||
      worker.model !== (config.worker.Model || '') ||
      worker.useDefault !== !config.worker.Model ||
      worker.enabled !== (config.worker.Enabled !== false) ||
      worker.timeout !== (config.worker.Timeout || '') ||
      resolver.model !== (config.resolver.Model || '') ||
      resolver.useDefault !== !config.resolver.Model ||
      resolver.enabled !== (config.resolver.Enabled !== false) ||
      resolver.timeout !== (config.resolver.Timeout || '') ||
      repair.model !== (config.repair.Model || '') ||
      repair.useDefault !== !config.repair.Model ||
      repair.enabled !== (config.repair.Enabled !== false) ||
      repair.timeout !== (config.repair.Timeout || '');
    setHasChanges(changed);
  }, [config, defaultModel, customDefaultModel, worker, resolver, repair]);

  // Handle escape key
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen && !isSaving) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, isSaving, onClose]);

  // Handle click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dialogRef.current && !dialogRef.current.contains(event.target as Node) && !isSaving) {
        onClose();
      }
    };
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
    }
  }, [isOpen, isSaving, onClose]);

  if (!isOpen) return null;

  const handleSave = async () => {
    const effectiveDefault = defaultModel === 'custom' ? customDefaultModel : defaultModel;
    const req: AgentConfigUpdateRequest = {
      default_model: effectiveDefault,
      worker: {
        Model: worker.useDefault ? '' : worker.model,
        Enabled: worker.enabled,
        ...(worker.timeout && { Timeout: worker.timeout }),
      },
      resolver: {
        Model: resolver.useDefault ? '' : resolver.model,
        Enabled: resolver.enabled,
        ...(resolver.timeout && { Timeout: resolver.timeout }),
      },
      repair: {
        Model: repair.useDefault ? '' : repair.model,
        Enabled: repair.enabled,
        ...(repair.timeout && { Timeout: repair.timeout }),
      },
    };
    const success = await update(req);
    if (success) {
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    }
  };

  const handlePersist = async () => {
    await persist();
  };

  const updateAgentType = (
    setter: React.Dispatch<React.SetStateAction<AgentTypeFormState>>,
    field: keyof AgentTypeFormState,
    value: string | boolean
  ) => {
    setter((prev) => ({ ...prev, [field]: value }));
  };

  const renderModelSelector = (
    label: string,
    state: AgentTypeFormState,
    setter: React.Dispatch<React.SetStateAction<AgentTypeFormState>>
  ) => {
    const effectiveDefault = defaultModel === 'custom' ? customDefaultModel : defaultModel;
    return (
      <div className="p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <Bot className="w-4 h-4 text-gray-500 dark:text-gray-400" />
            <span className="font-mono font-normal text-sm text-gray-900 dark:text-gray-100">
              {label}
            </span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={state.enabled}
            onClick={() => updateAgentType(setter, 'enabled', !state.enabled)}
            className={`
              relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent
              transition-colors duration-200 ease-in-out
              ${state.enabled ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'}
            `}
          >
            <span
              className={`
                pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0
                transition duration-200 ease-in-out
                ${state.enabled ? 'translate-x-4' : 'translate-x-0'}
              `}
            />
          </button>
        </div>

        <div className={`space-y-3 ${!state.enabled ? 'opacity-50 pointer-events-none' : ''}`}>
          {/* Use default checkbox */}
          <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
            <input
              type="checkbox"
              checked={state.useDefault}
              onChange={(e) => {
                updateAgentType(setter, 'useDefault', e.target.checked);
                if (e.target.checked) {
                  updateAgentType(setter, 'model', '');
                }
              }}
              className="rounded border-gray-300 dark:border-gray-600 text-blue-600"
            />
            Use default model
            {effectiveDefault && (
              <span className="text-xs text-gray-500 dark:text-gray-400 font-mono">
                ({effectiveDefault || 'CLI default'})
              </span>
            )}
          </label>

          {/* Model override */}
          {!state.useDefault && (
            <div>
              <label className="block text-xs text-gray-600 dark:text-gray-400 mb-1">Model override</label>
              <select
                value={KNOWN_MODELS.includes(state.model) ? state.model : 'custom'}
                onChange={(e) => {
                  if (e.target.value === 'custom') {
                    // Keep current model as custom input
                  } else {
                    updateAgentType(setter, 'model', e.target.value);
                  }
                }}
                className="w-full px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
              >
                {KNOWN_MODELS.filter(m => m !== '').map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
                <option value="custom">Custom...</option>
              </select>
              {(!KNOWN_MODELS.includes(state.model) || state.model === '') && !state.useDefault && (
                <input
                  type="text"
                  value={state.model}
                  onChange={(e) => updateAgentType(setter, 'model', e.target.value)}
                  placeholder="Enter model ID"
                  className="w-full mt-1 px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
                />
              )}
            </div>
          )}

          {/* Timeout */}
          <div>
            <label className="block text-xs text-gray-600 dark:text-gray-400 mb-1">Timeout</label>
            <input
              type="text"
              value={state.timeout}
              onChange={(e) => updateAgentType(setter, 'timeout', e.target.value)}
              placeholder="e.g., 10m, 30m, 1h"
              className="w-full px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
            />
          </div>
        </div>
      </div>
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        ref={dialogRef}
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[90vh] flex flex-col"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <Settings className="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100">
              Agent Configuration
            </h2>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={reload}
              disabled={isLoading}
              className="p-1.5 text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              disabled={isSaving}
              className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-50"
            >
              <X className="w-5 h-5 text-gray-500 dark:text-gray-400" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-5">
          {/* Error */}
          {error && (
            <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm">
              <AlertTriangle className="w-4 h-4 flex-shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Loading */}
          {isLoading && !config && (
            <div className="text-center py-8 text-gray-500 dark:text-gray-400">
              <Loader2 className="w-6 h-6 animate-spin mx-auto mb-2" />
              <span className="text-sm font-mono">Loading agent config...</span>
            </div>
          )}

          {config && (
            <>
              {/* Default Model */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Default Model
                </label>
                <select
                  value={KNOWN_MODELS.includes(defaultModel) ? defaultModel : 'custom'}
                  onChange={(e) => {
                    setDefaultModel(e.target.value);
                    if (e.target.value !== 'custom') {
                      setCustomDefaultModel('');
                    }
                  }}
                  className="w-full px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
                >
                  <option value="">CLI default</option>
                  {KNOWN_MODELS.filter(m => m !== '').map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
                  <option value="custom">Custom...</option>
                </select>
                {defaultModel === 'custom' && (
                  <input
                    type="text"
                    value={customDefaultModel}
                    onChange={(e) => setCustomDefaultModel(e.target.value)}
                    placeholder="Enter model ID"
                    className="w-full mt-1 px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
                  />
                )}
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Model used for all agent types unless overridden below
                </p>
              </div>

              {/* Agent type sections */}
              {renderModelSelector('Worker', worker, setWorker)}
              {renderModelSelector('Resolver', resolver, setResolver)}
              {renderModelSelector('Repair', repair, setRepair)}
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-2">
            {config && !config.persisted && (
              <button
                onClick={handlePersist}
                disabled={isPersisting || hasChanges}
                className={`
                  flex items-center gap-1 px-3 py-2 text-sm font-mono rounded transition-colors
                  ${isPersisting
                    ? 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-600 dark:text-yellow-400 opacity-50 cursor-not-allowed'
                    : hasChanges
                      ? 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500 cursor-not-allowed'
                      : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 hover:bg-yellow-200 dark:hover:bg-yellow-900/50'
                  }
                `}
                title={hasChanges ? 'Save changes first' : 'Save to config file'}
              >
                <Save className={`w-3.5 h-3.5 ${isPersisting ? 'animate-pulse' : ''}`} />
                <span>Persist</span>
              </button>
            )}
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={onClose}
              disabled={isSaving}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              disabled={isSaving || !hasChanges}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isSaving ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Saving...
                </>
              ) : saveSuccess ? (
                <>
                  <Check className="w-4 h-4" />
                  Saved
                </>
              ) : (
                'Apply'
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
