import React, { useState, useEffect, useRef } from 'react';
import { X, Settings, Loader2, ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react';
import type { RunConfig, RuleOverride } from '../../stores/stateStore';
import { DEFAULT_RUN_CONFIG } from '../../stores/stateStore';

type DialogMode = 'start' | 'configure';

interface RunConfigDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onStart: (config: RunConfig) => void;
  onSave?: (config: RunConfig) => void;
  isStarting: boolean;
  isSaving?: boolean;
  initialConfig?: RunConfig;
  repoName?: string | undefined;
  mode?: DialogMode;
}

// Default empty rule for new rule creation
const DEFAULT_RULE: RuleOverride = {
  name: '',
  condition: '',
  action: 'deny',
  enabled: true,
};

export const RunConfigDialog: React.FC<RunConfigDialogProps> = ({
  isOpen,
  onClose,
  onStart,
  onSave,
  isStarting,
  isSaving = false,
  initialConfig = DEFAULT_RUN_CONFIG,
  repoName,
  mode = 'start',
}) => {
  const [config, setConfig] = useState<RunConfig>(initialConfig);
  const [rulesExpanded, setRulesExpanded] = useState(false);
  const [newRule, setNewRule] = useState<RuleOverride>(DEFAULT_RULE);
  const [showAddRule, setShowAddRule] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);

  // Reset config when dialog opens
  useEffect(() => {
    if (isOpen) {
      setConfig(initialConfig);
      setRulesExpanded(false);
      setShowAddRule(false);
      setNewRule(DEFAULT_RULE);
    }
  }, [isOpen, initialConfig]);

  // Helper to add a new rule
  const handleAddRule = () => {
    if (!newRule.name || !newRule.condition) return;
    setConfig({
      ...config,
      rule_overrides: [...(config.rule_overrides || []), { ...newRule }],
    });
    setNewRule(DEFAULT_RULE);
    setShowAddRule(false);
  };

  // Helper to remove a rule
  const handleRemoveRule = (index: number) => {
    const rules = [...(config.rule_overrides || [])];
    rules.splice(index, 1);
    setConfig({ ...config, rule_overrides: rules });
  };

  // Helper to toggle a rule's enabled state
  const handleToggleRule = (index: number) => {
    const rules = [...(config.rule_overrides || [])];
    const currentRule = rules[index];
    if (currentRule) {
      rules[index] = { ...currentRule, enabled: currentRule.enabled !== false ? false : true };
      setConfig({ ...config, rule_overrides: rules });
    }
  };

  const isProcessing = isStarting || isSaving;

  // Handle escape key
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen && !isProcessing) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, isProcessing, onClose]);

  // Handle click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        dialogRef.current &&
        !dialogRef.current.contains(event.target as Node) &&
        !isProcessing
      ) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
    }
  }, [isOpen, isProcessing, onClose]);

  if (!isOpen) return null;

  const handleStart = () => {
    onStart(config);
  };

  const handleSave = () => {
    if (onSave) {
      onSave(config);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        ref={dialogRef}
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <Settings className="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100">
              {mode === 'configure' ? 'Configure Run Settings' : 'Start Run'}
            </h2>
          </div>
          <button
            onClick={onClose}
            disabled={isProcessing}
            className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5 text-gray-500 dark:text-gray-400" />
          </button>
        </div>

        {/* Content */}
        <div className="px-6 py-4 space-y-5">
          {repoName && (
            <div className="text-sm text-gray-600 dark:text-gray-400">
              Repository: <span className="font-medium text-gray-900 dark:text-gray-100">{repoName}</span>
            </div>
          )}

          {/* Concurrency */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Concurrency
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="1"
                max="16"
                value={config.concurrency}
                onChange={(e) => setConfig({ ...config, concurrency: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                {config.concurrency}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Number of agents running in parallel
            </p>
          </div>

          {/* Max Priority */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Max Priority (P0-P4)
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="0"
                max="4"
                value={config.max_priority}
                onChange={(e) => setConfig({ ...config, max_priority: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                P{config.max_priority}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Only process tasks up to this priority level
            </p>
          </div>

          {/* Max Retries */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Max Retries
            </label>
            <div className="flex items-center gap-3">
              <input
                type="range"
                min="0"
                max="5"
                value={config.max_retries}
                onChange={(e) => setConfig({ ...config, max_retries: Number(e.target.value) })}
                className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer"
              />
              <span className="w-8 text-center font-mono text-sm text-gray-900 dark:text-gray-100">
                {config.max_retries}
              </span>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Retry failed tasks up to this many times
            </p>
          </div>

          {/* Sandbox Toggle */}
          <div className="flex items-center justify-between">
            <div>
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Sandbox Mode (bwrap)
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Isolate agents in sandboxed environments
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={config.use_bwrap}
              onClick={() => setConfig({ ...config, use_bwrap: !config.use_bwrap })}
              className={`
                relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent
                transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2
                ${config.use_bwrap ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'}
              `}
            >
              <span
                className={`
                  pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0
                  transition duration-200 ease-in-out
                  ${config.use_bwrap ? 'translate-x-5' : 'translate-x-0'}
                `}
              />
            </button>
          </div>

          {/* Rules Overrides Section */}
          <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
            <button
              type="button"
              onClick={() => setRulesExpanded(!rulesExpanded)}
              className="flex items-center justify-between w-full text-left"
            >
              <div>
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Rule Overrides
                </span>
                {(config.rule_overrides?.length ?? 0) > 0 && (
                  <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                    ({config.rule_overrides?.length} rule{config.rule_overrides?.length !== 1 ? 's' : ''})
                  </span>
                )}
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Custom rules for this run only (highest precedence)
                </p>
              </div>
              {rulesExpanded ? (
                <ChevronUp className="w-5 h-5 text-gray-500" />
              ) : (
                <ChevronDown className="w-5 h-5 text-gray-500" />
              )}
            </button>

            {rulesExpanded && (
              <div className="mt-3 space-y-3">
                {/* Existing rules */}
                {(config.rule_overrides || []).map((rule, index) => (
                  <div
                    key={index}
                    className={`p-3 rounded-lg border ${
                      rule.enabled !== false
                        ? 'border-gray-200 dark:border-gray-600 bg-gray-50 dark:bg-gray-700'
                        : 'border-gray-200 dark:border-gray-700 bg-gray-100 dark:bg-gray-800 opacity-60'
                    }`}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                        {rule.name}
                      </span>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => handleToggleRule(index)}
                          className={`px-2 py-0.5 text-xs rounded ${
                            rule.enabled !== false
                              ? 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
                              : 'bg-gray-200 text-gray-600 dark:bg-gray-600 dark:text-gray-400'
                          }`}
                        >
                          {rule.enabled !== false ? 'Enabled' : 'Disabled'}
                        </button>
                        <button
                          type="button"
                          onClick={() => handleRemoveRule(index)}
                          className="p-1 text-red-500 hover:text-red-700 dark:hover:text-red-400"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                    <div className="text-xs text-gray-600 dark:text-gray-400">
                      <span className={`inline-block px-1.5 py-0.5 rounded mr-2 ${
                        rule.action === 'deny'
                          ? 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300'
                          : 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
                      }`}>
                        {rule.action.toUpperCase()}
                      </span>
                      <code className="bg-gray-200 dark:bg-gray-600 px-1 rounded">{rule.condition}</code>
                    </div>
                  </div>
                ))}

                {/* Add new rule form */}
                {showAddRule ? (
                  <div className="p-3 rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/30">
                    <div className="space-y-2">
                      <input
                        type="text"
                        placeholder="Rule name"
                        value={newRule.name}
                        onChange={(e) => setNewRule({ ...newRule, name: e.target.value })}
                        className="w-full px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
                      />
                      <input
                        type="text"
                        placeholder="Condition (e.g., priority > 2)"
                        value={newRule.condition}
                        onChange={(e) => setNewRule({ ...newRule, condition: e.target.value })}
                        className="w-full px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono"
                      />
                      <div className="flex items-center gap-2">
                        <select
                          value={newRule.action}
                          onChange={(e) => setNewRule({ ...newRule, action: e.target.value as 'deny' | 'allow' })}
                          className="px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
                        >
                          <option value="deny">Deny</option>
                          <option value="allow">Allow</option>
                        </select>
                        <div className="flex-1" />
                        <button
                          type="button"
                          onClick={() => setShowAddRule(false)}
                          className="px-2 py-1 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
                        >
                          Cancel
                        </button>
                        <button
                          type="button"
                          onClick={handleAddRule}
                          disabled={!newRule.name || !newRule.condition}
                          className="px-2 py-1 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          Add
                        </button>
                      </div>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setShowAddRule(true)}
                    className="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-600 dark:text-gray-400 border border-dashed border-gray-300 dark:border-gray-600 rounded-lg hover:border-gray-400 dark:hover:border-gray-500 hover:text-gray-800 dark:hover:text-gray-300"
                  >
                    <Plus className="w-4 h-4" />
                    Add rule override
                  </button>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={onClose}
            disabled={isProcessing}
            className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
          >
            Cancel
          </button>
          {mode === 'configure' ? (
            <button
              onClick={handleSave}
              disabled={isSaving}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isSaving ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Saving...
                </>
              ) : (
                'Save'
              )}
            </button>
          ) : (
            <button
              onClick={handleStart}
              disabled={isStarting}
              className="px-4 py-2 text-sm font-medium text-white bg-green-600 rounded-lg hover:bg-green-700 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isStarting ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Starting...
                </>
              ) : (
                'Start Run'
              )}
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
