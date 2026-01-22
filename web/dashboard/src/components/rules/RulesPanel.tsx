import React, { useEffect, useState, useCallback } from 'react';
import { Filter, Plus, RefreshCw, ChevronDown, AlertTriangle } from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';
import {
  getRules,
  addRule,
  updateRule,
  deleteRule,
  updateRulesConfig,
  persistRule,
} from '../../api/client';
import type { AddRuleRequest, UpdateConfigRequest, RuntimeRule } from '../../api/client';
import { RuleItem } from './RuleItem';
import { ConfigSettingsCard } from './ConfigSettingsCard';
import { AddRuleDialog } from './AddRuleDialog';

interface RulesPanelProps {
  isExpanded: boolean;
  onToggle: () => void;
  width: number;
  isResizing: boolean;
  onResizeStart: (e: React.MouseEvent) => void;
}

export const RulesPanel: React.FC<RulesPanelProps> = ({
  isExpanded,
  onToggle,
  width,
  isResizing,
  onResizeStart,
}) => {
  const [isUpdating, setIsUpdating] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  // State from store
  const configRules = useStateStore((state) => state.configRules);
  const customRules = useStateStore((state) => state.customRules);
  const runtimeRules = useStateStore((state) => state.runtimeRules);
  const isRulesLoading = useStateStore((state) => state.isRulesLoading);
  const showAddRuleDialog = useStateStore((state) => state.showAddRuleDialog);
  const setRulesState = useStateStore((state) => state.setRulesState);
  const setRulesLoading = useStateStore((state) => state.setRulesLoading);
  const setShowAddRuleDialog = useStateStore((state) => state.setShowAddRuleDialog);
  const addRuntimeRule = useStateStore((state) => state.addRuntimeRule);
  const updateRuntimeRule = useStateStore((state) => state.updateRuntimeRule);
  const removeRuntimeRule = useStateStore((state) => state.removeRuntimeRule);
  const persistRuntimeRule = useStateStore((state) => state.persistRuntimeRule);
  const updateConfigRules = useStateStore((state) => state.updateConfigRules);

  // Load rules on mount
  const loadRules = useCallback(async () => {
    try {
      setRulesLoading(true);
      setLoadError(null);
      const response = await getRules();
      setRulesState(response.config_rules, response.custom_rules, response.runtime_rules);
    } catch (error) {
      console.error('Failed to load rules:', error);
      setLoadError(error instanceof Error ? error.message : 'Failed to load rules');
    } finally {
      setRulesLoading(false);
    }
  }, [setRulesState, setRulesLoading]);

  useEffect(() => {
    if (isExpanded) {
      loadRules();
    }
  }, [isExpanded, loadRules]);

  // Handle adding a new rule
  const handleAddRule = async (request: AddRuleRequest): Promise<{ success: boolean; error?: string }> => {
    try {
      const response = await addRule(request);
      if (response.success && response.rule) {
        addRuntimeRule(response.rule);
        return { success: true };
      }
      const result: { success: boolean; error?: string } = { success: false };
      if (response.error) {
        result.error = response.error;
      }
      return result;
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to add rule',
      };
    }
  };

  // Handle toggling a rule
  const handleToggleRule = async (name: string, enabled: boolean) => {
    try {
      setIsUpdating(name);
      const response = await updateRule(name, { enabled });
      if (response.success) {
        updateRuntimeRule(name, enabled);
      }
    } catch (error) {
      console.error('Failed to toggle rule:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Handle deleting a rule
  const handleDeleteRule = async (name: string) => {
    try {
      setIsUpdating(name);
      const response = await deleteRule(name);
      if (response.success) {
        removeRuntimeRule(name);
      }
    } catch (error) {
      console.error('Failed to delete rule:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Handle persisting a rule to config
  const handlePersistRule = async (name: string) => {
    try {
      setIsUpdating(name);
      const response = await persistRule(name);
      if (response.success && response.rule) {
        persistRuntimeRule(name, response.rule);
      }
    } catch (error) {
      console.error('Failed to persist rule:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Handle updating config
  const handleUpdateConfig = async (update: UpdateConfigRequest) => {
    try {
      setIsUpdating('config');
      const response = await updateRulesConfig(update);
      if (response.success && response.config_rules) {
        updateConfigRules(response.config_rules);
      }
    } catch (error) {
      console.error('Failed to update config:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Combine all rules for display
  const allRules: RuntimeRule[] = [...customRules, ...runtimeRules];
  const activeRuleCount = allRules.filter((r) => r.enabled !== false).length;

  // Collapsed view
  if (!isExpanded) {
    return (
      <div className="flex flex-col border-r border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
        <button
          onClick={onToggle}
          className="p-3 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          title="Expand Rules Panel"
        >
          <Filter className="w-5 h-5 text-gray-500 dark:text-gray-400" />
        </button>
        {activeRuleCount > 0 && (
          <div className="px-3 pb-2">
            <span className="inline-flex items-center justify-center w-5 h-5 text-xs font-mono bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded-full">
              {activeRuleCount}
            </span>
          </div>
        )}
      </div>
    );
  }

  return (
    <>
      <div
        className="flex flex-col border-r border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 overflow-hidden"
        style={{ width: `${width}px` }}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
          <button
            onClick={onToggle}
            className="flex items-center gap-2 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
          >
            <ChevronDown className="w-4 h-4 text-gray-400" />
            <Filter className="w-4 h-4 text-gray-500 dark:text-gray-400" />
            <span className="font-mono font-normal text-sm text-gray-900 dark:text-gray-100">
              Rules
            </span>
            {activeRuleCount > 0 && (
              <span className="px-1.5 py-0.5 text-xs font-mono bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded">
                {activeRuleCount}
              </span>
            )}
          </button>
          <div className="flex items-center gap-1">
            <button
              onClick={loadRules}
              disabled={isRulesLoading}
              className="p-1.5 text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
              title="Refresh rules"
            >
              <RefreshCw className={`w-4 h-4 ${isRulesLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={() => setShowAddRuleDialog(true)}
              className="p-1.5 text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
              title="Add rule"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Loading state */}
          {isRulesLoading && !configRules && (
            <div className="text-center py-8 text-gray-500 dark:text-gray-400">
              <RefreshCw className="w-6 h-6 animate-spin mx-auto mb-2" />
              <span className="text-sm font-mono">Loading rules...</span>
            </div>
          )}

          {/* Error state */}
          {loadError && (
            <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm">
              <AlertTriangle className="w-4 h-4 flex-shrink-0" />
              <span>{loadError}</span>
            </div>
          )}

          {/* Config Settings */}
          {!isRulesLoading && (
            <ConfigSettingsCard
              config={configRules}
              onUpdate={handleUpdateConfig}
              isUpdating={isUpdating === 'config'}
            />
          )}

          {/* Rules List */}
          {!isRulesLoading && allRules.length > 0 && (
            <div>
              <h3 className="text-xs font-mono text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">
                Active Rules ({allRules.length})
              </h3>
              <div className="space-y-2">
                {allRules.map((rule) => {
                  const baseProps = {
                    key: `${rule.source}-${rule.name}`,
                    rule,
                    onToggle: handleToggleRule,
                    isUpdating: isUpdating === rule.name,
                  };
                  // Conditionally pass onDelete and onPersist only for runtime rules
                  return rule.source === 'runtime' ? (
                    <RuleItem {...baseProps} onDelete={handleDeleteRule} onPersist={handlePersistRule} />
                  ) : (
                    <RuleItem {...baseProps} />
                  );
                })}
              </div>
            </div>
          )}

          {/* Empty state */}
          {!isRulesLoading && !loadError && allRules.length === 0 && (
            <div className="text-center py-8 text-gray-500 dark:text-gray-400">
              <Filter className="w-8 h-8 mx-auto mb-2 opacity-50" />
              <p className="text-sm font-mono">No custom rules defined</p>
              <button
                onClick={() => setShowAddRuleDialog(true)}
                className="mt-2 text-sm font-mono text-blue-600 dark:text-blue-400 hover:underline"
              >
                Add a rule
              </button>
            </div>
          )}
        </div>

        {/* Resize handle */}
        <div
          className={`
            absolute top-0 right-0 w-1 h-full cursor-ew-resize
            hover:bg-blue-500/50 transition-colors
            ${isResizing ? 'bg-blue-500' : ''}
          `}
          onMouseDown={onResizeStart}
        />
      </div>

      {/* Add Rule Dialog */}
      <AddRuleDialog
        isOpen={showAddRuleDialog}
        onClose={() => setShowAddRuleDialog(false)}
        onAdd={handleAddRule}
        isAdding={isUpdating === 'add'}
      />
    </>
  );
};
