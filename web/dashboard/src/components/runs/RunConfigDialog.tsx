import React, { useState, useEffect, useRef } from 'react';
import { X, Settings, Loader2, ChevronDown, ChevronUp, Plus, Trash2, FileText, Zap } from 'lucide-react';
import type { RunConfig, RuleOverride } from '../../stores/stateStore';
import { DEFAULT_RUN_CONFIG } from '../../stores/stateStore';
import type { Rule } from '../../api/client';

type DialogMode = 'start' | 'configure';

// Extended rule for the dialog that tracks config rules and their override state
interface DialogRule {
  name: string;
  condition: string;
  action: 'deny' | 'allow';
  enabled: boolean;
  source: 'config' | 'override'; // Where the rule came from
  modified: boolean; // Whether it's been modified from config
}

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
  configRules?: Rule[]; // Rules loaded from config file
  isLoadingRules?: boolean;
}

// Default empty rule for new rule creation
const DEFAULT_RULE: RuleOverride = {
  name: '',
  condition: '',
  action: 'deny',
  enabled: true,
};

// Helper to convert config rules to dialog rules
function configRulesToDialogRules(configRules: Rule[]): DialogRule[] {
  return configRules.map((rule) => ({
    name: rule.name,
    condition: rule.condition,
    action: rule.action,
    enabled: rule.enabled,
    source: 'config' as const,
    modified: false,
  }));
}

// Helper to convert rule overrides to dialog rules
function overridesToDialogRules(overrides: RuleOverride[]): DialogRule[] {
  return overrides.map((override) => ({
    name: override.name,
    condition: override.condition,
    action: override.action,
    enabled: override.enabled !== false,
    source: 'override' as const,
    modified: false,
  }));
}

// Helper to convert dialog rules back to rule overrides for the config
function dialogRulesToOverrides(dialogRules: DialogRule[], originalConfigRules: Rule[]): RuleOverride[] {
  const overrides: RuleOverride[] = [];

  for (const dialogRule of dialogRules) {
    if (dialogRule.source === 'override') {
      // Runtime-only rules always get included
      overrides.push({
        name: dialogRule.name,
        condition: dialogRule.condition,
        action: dialogRule.action,
        enabled: dialogRule.enabled,
      });
    } else if (dialogRule.modified) {
      // Config rules that were modified become overrides
      const originalRule = originalConfigRules.find((r) => r.name === dialogRule.name);
      if (originalRule) {
        // Only include if actually different from config
        if (originalRule.enabled !== dialogRule.enabled ||
            originalRule.action !== dialogRule.action) {
          overrides.push({
            name: dialogRule.name,
            condition: dialogRule.condition,
            action: dialogRule.action,
            enabled: dialogRule.enabled,
          });
        }
      }
    }
  }

  return overrides;
}

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
  configRules = [],
  isLoadingRules = false,
}) => {
  const [config, setConfig] = useState<RunConfig>(initialConfig);
  const [dialogRules, setDialogRules] = useState<DialogRule[]>([]);
  const [rulesExpanded, setRulesExpanded] = useState(false);
  const [newRule, setNewRule] = useState<RuleOverride>(DEFAULT_RULE);
  const [showAddRule, setShowAddRule] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);

  // Reset config and merge rules when dialog opens
  useEffect(() => {
    if (isOpen) {
      setConfig(initialConfig);

      // Merge config rules with any existing overrides
      const configDialogRules = configRulesToDialogRules(configRules);
      const overrideDialogRules = overridesToDialogRules(initialConfig.rule_overrides || []);

      // Apply overrides to config rules
      const mergedRules = configDialogRules.map((configRule) => {
        const override = overrideDialogRules.find((o) => o.name === configRule.name);
        if (override) {
          return {
            ...configRule,
            enabled: override.enabled,
            action: override.action,
            modified: true,
          };
        }
        return configRule;
      });

      // Add any override-only rules (not in config)
      const configRuleNames = new Set(configDialogRules.map((r) => r.name));
      const additionalOverrides = overrideDialogRules.filter((o) => !configRuleNames.has(o.name));

      setDialogRules([...mergedRules, ...additionalOverrides]);
      setRulesExpanded(configRules.length > 0 || (initialConfig.rule_overrides?.length ?? 0) > 0);
      setShowAddRule(false);
      setNewRule(DEFAULT_RULE);
    }
  }, [isOpen, initialConfig, configRules]);

  // Helper to add a new rule
  const handleAddRule = () => {
    if (!newRule.name || !newRule.condition) return;
    const newDialogRule: DialogRule = {
      name: newRule.name,
      condition: newRule.condition,
      action: newRule.action,
      enabled: newRule.enabled !== false,
      source: 'override',
      modified: false,
    };
    setDialogRules([...dialogRules, newDialogRule]);
    setNewRule(DEFAULT_RULE);
    setShowAddRule(false);
  };

  // Helper to remove a rule (only for override/runtime rules)
  const handleRemoveRule = (index: number) => {
    const rule = dialogRules[index];
    if (rule && rule.source === 'override') {
      const newRules = [...dialogRules];
      newRules.splice(index, 1);
      setDialogRules(newRules);
    }
  };

  // Helper to toggle a rule's enabled state
  const handleToggleRule = (index: number) => {
    const newRules = [...dialogRules];
    const currentRule = newRules[index];
    if (currentRule) {
      newRules[index] = {
        ...currentRule,
        enabled: !currentRule.enabled,
        modified: currentRule.source === 'config' ? true : currentRule.modified,
      };
      setDialogRules(newRules);
    }
  };

  // Helper to reset a config rule to its original state
  const handleResetRule = (index: number) => {
    const rule = dialogRules[index];
    if (rule && rule.source === 'config') {
      const originalRule = configRules.find((r) => r.name === rule.name);
      if (originalRule) {
        const newRules = [...dialogRules];
        newRules[index] = {
          name: originalRule.name,
          condition: originalRule.condition,
          action: originalRule.action,
          enabled: originalRule.enabled,
          source: 'config',
          modified: false,
        };
        setDialogRules(newRules);
      }
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

  // Build the final config with rule overrides
  const buildFinalConfig = (): RunConfig => {
    const ruleOverrides = dialogRulesToOverrides(dialogRules, configRules);
    return {
      ...config,
      rule_overrides: ruleOverrides,
    };
  };

  const handleStart = () => {
    onStart(buildFinalConfig());
  };

  const handleSave = () => {
    if (onSave) {
      onSave(buildFinalConfig());
    }
  };

  // Count active rules and modified rules
  const activeRuleCount = dialogRules.filter((r) => r.enabled).length;
  const configRuleCount = dialogRules.filter((r) => r.source === 'config').length;
  const overrideRuleCount = dialogRules.filter((r) => r.source === 'override').length;
  const modifiedRuleCount = dialogRules.filter((r) => r.source === 'config' && r.modified).length;

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

          {/* Rules Section */}
          <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
            <button
              type="button"
              onClick={() => setRulesExpanded(!rulesExpanded)}
              className="flex items-center justify-between w-full text-left"
            >
              <div>
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Rules
                </span>
                {dialogRules.length > 0 && (
                  <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                    ({activeRuleCount}/{dialogRules.length} active)
                  </span>
                )}
                {modifiedRuleCount > 0 && (
                  <span className="ml-1 text-xs text-yellow-600 dark:text-yellow-400">
                    ({modifiedRuleCount} modified)
                  </span>
                )}
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Configure which rules apply to this run
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
                {/* Loading state */}
                {isLoadingRules && dialogRules.length === 0 && (
                  <div className="flex items-center justify-center py-4 text-gray-500 dark:text-gray-400">
                    <Loader2 className="w-4 h-4 animate-spin mr-2" />
                    <span className="text-sm">Loading rules...</span>
                  </div>
                )}

                {/* Config rules section */}
                {configRuleCount > 0 && (
                  <div className="space-y-2">
                    <div className="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                      <FileText className="w-3.5 h-3.5" />
                      <span>From Config ({configRuleCount})</span>
                    </div>
                    {dialogRules.filter((r) => r.source === 'config').map((rule) => {
                      const originalIndex = dialogRules.findIndex((r) => r.name === rule.name);
                      return (
                        <div
                          key={rule.name}
                          className={`p-3 rounded-lg border ${
                            rule.enabled
                              ? rule.modified
                                ? 'border-yellow-200 dark:border-yellow-800 bg-yellow-50 dark:bg-yellow-900/20'
                                : 'border-gray-200 dark:border-gray-600 bg-gray-50 dark:bg-gray-700'
                              : 'border-gray-200 dark:border-gray-700 bg-gray-100 dark:bg-gray-800 opacity-60'
                          }`}
                        >
                          <div className="flex items-center justify-between mb-2">
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                                {rule.name}
                              </span>
                              {rule.modified && (
                                <span className="text-xs px-1.5 py-0.5 bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300 rounded">
                                  modified
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-2">
                              {rule.modified && (
                                <button
                                  type="button"
                                  onClick={() => handleResetRule(originalIndex)}
                                  className="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
                                  title="Reset to config value"
                                >
                                  Reset
                                </button>
                              )}
                              <button
                                type="button"
                                onClick={() => handleToggleRule(originalIndex)}
                                className={`px-2 py-0.5 text-xs rounded transition-colors ${
                                  rule.enabled
                                    ? 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
                                    : 'bg-gray-200 text-gray-600 dark:bg-gray-600 dark:text-gray-400'
                                }`}
                              >
                                {rule.enabled ? 'Enabled' : 'Disabled'}
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
                            <code className="bg-gray-200 dark:bg-gray-600 px-1 rounded">
                              {rule.condition}
                            </code>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}

                {/* Runtime override rules section */}
                {overrideRuleCount > 0 && (
                  <div className="space-y-2">
                    <div className="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                      <Zap className="w-3.5 h-3.5" />
                      <span>Runtime Only ({overrideRuleCount})</span>
                    </div>
                    {dialogRules.filter((r) => r.source === 'override').map((rule) => {
                      const originalIndex = dialogRules.findIndex((r) => r.name === rule.name);
                      return (
                        <div
                          key={rule.name}
                          className={`p-3 rounded-lg border ${
                            rule.enabled
                              ? 'border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20'
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
                                onClick={() => handleToggleRule(originalIndex)}
                                className={`px-2 py-0.5 text-xs rounded transition-colors ${
                                  rule.enabled
                                    ? 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
                                    : 'bg-gray-200 text-gray-600 dark:bg-gray-600 dark:text-gray-400'
                                }`}
                              >
                                {rule.enabled ? 'Enabled' : 'Disabled'}
                              </button>
                              <button
                                type="button"
                                onClick={() => handleRemoveRule(originalIndex)}
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
                            <code className="bg-gray-200 dark:bg-gray-600 px-1 rounded">
                              {rule.condition}
                            </code>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}

                {/* Empty state */}
                {!isLoadingRules && dialogRules.length === 0 && (
                  <div className="text-center py-4 text-gray-500 dark:text-gray-400">
                    <p className="text-sm">No rules configured</p>
                    <p className="text-xs mt-1">Add rules below to filter tasks for this run</p>
                  </div>
                )}

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
