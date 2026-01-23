import React, { useEffect, useState, useCallback } from 'react';
import { Filter, Plus, RefreshCw, ChevronDown, AlertTriangle, Save } from 'lucide-react';
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { useStateStore } from '../../stores/stateStore';
import {
  getRules,
  addRule,
  updateRule,
  deleteRule,
  reorderRule,
  saveRules,
} from '../../api/client';
import type { AddRuleRequest, Rule } from '../../api/client';
import { RuleItem } from './RuleItem';
import { AddRuleDialog } from './AddRuleDialog';
import { EditRuleModal } from './EditRuleModal';

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
  const [editingRule, setEditingRule] = useState<Rule | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  // State from store - unified format
  const rules = useStateStore((state) => state.rules);
  const rulesPersistedState = useStateStore((state) => state.rulesPersistedState);
  const isRulesLoading = useStateStore((state) => state.isRulesLoading);
  const showAddRuleDialog = useStateStore((state) => state.showAddRuleDialog);
  const setRulesState = useStateStore((state) => state.setRulesState);
  const setRulesLoading = useStateStore((state) => state.setRulesLoading);
  const setShowAddRuleDialog = useStateStore((state) => state.setShowAddRuleDialog);
  const storeAddRule = useStateStore((state) => state.addRule);
  const storeUpdateRule = useStateStore((state) => state.updateRule);
  const storeRemoveRule = useStateStore((state) => state.removeRule);
  const storeReorderRules = useStateStore((state) => state.reorderRules);
  const repositories = useStateStore((state) => state.repositories);
  const activeRepoId = useStateStore((state) => state.activeRepoId);

  // Get the active repository path
  const activeRepo = repositories.find((r) => r.id === activeRepoId);
  const repoPath = activeRepo?.path ?? '';

  // DnD sensors
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8, // Require 8px of movement before starting drag
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  // Load rules on mount
  const loadRules = useCallback(async () => {
    if (!repoPath) {
      setLoadError('No active repository selected');
      return;
    }
    try {
      setRulesLoading(true);
      setLoadError(null);
      const response = await getRules(repoPath);
      setRulesState(response.rules, response.persisted);
    } catch (error) {
      console.error('Failed to load rules:', error);
      setLoadError(error instanceof Error ? error.message : 'Failed to load rules');
    } finally {
      setRulesLoading(false);
    }
  }, [repoPath, setRulesState, setRulesLoading]);

  useEffect(() => {
    if (isExpanded) {
      loadRules();
    }
  }, [isExpanded, loadRules]);

  // Handle adding a new rule
  const handleAddRule = async (request: AddRuleRequest): Promise<{ success: boolean; error?: string }> => {
    if (!repoPath) {
      return { success: false, error: 'No active repository selected' };
    }
    try {
      const response = await addRule(repoPath, request);
      if (response.success && response.rule) {
        storeAddRule(response.rule);
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
    if (!repoPath) return;
    try {
      setIsUpdating(name);
      const response = await updateRule(repoPath, name, { enabled });
      if (response.success) {
        storeUpdateRule(name, { enabled });
      }
    } catch (error) {
      console.error('Failed to toggle rule:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Handle deleting a rule
  const handleDeleteRule = async (name: string) => {
    if (!repoPath) return;
    try {
      setIsUpdating(name);
      const response = await deleteRule(repoPath, name);
      if (response.success) {
        storeRemoveRule(name);
      }
    } catch (error) {
      console.error('Failed to delete rule:', error);
    } finally {
      setIsUpdating(null);
    }
  };

  // Handle drag end for reordering
  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;

    if (!over || active.id === over.id || !repoPath) {
      return;
    }

    const oldIndex = rules.findIndex((r) => r.name === active.id);
    const newIndex = rules.findIndex((r) => r.name === over.id);

    if (oldIndex === -1 || newIndex === -1) {
      return;
    }

    // Optimistically update the UI
    storeReorderRules(oldIndex, newIndex);

    // Call the API to persist the reorder
    try {
      const response = await reorderRule(repoPath, active.id as string, newIndex);
      if (response.success && response.rules) {
        // Update state with server response
        setRulesState(response.rules, response.persisted ?? false);
      } else if (!response.success) {
        // Revert on failure by reloading
        console.error('Failed to reorder rule:', response.error);
        await loadRules();
      }
    } catch (error) {
      // Revert on error by reloading
      console.error('Failed to reorder rule:', error);
      await loadRules();
    }
  };

  // Handle editing a rule (opens modal)
  const handleEditRule = (rule: Rule) => {
    setEditingRule(rule);
  };

  // Handle saving edited rule from modal
  const handleSaveEditedRule = (updatedRule: Rule) => {
    storeUpdateRule(updatedRule.name, {
      conditions: updatedRule.conditions,
      action: updatedRule.action,
      enabled: updatedRule.enabled,
      persisted: false, // Mark as modified
    });
  };

  // Handle saving all rules to file
  const handleSaveRules = async () => {
    if (!repoPath) return;
    try {
      setIsSaving(true);
      const response = await saveRules(repoPath, { rules });
      if (response.success) {
        // Reload to get persisted state from server
        await loadRules();
      } else {
        console.error('Failed to save rules:', response.error);
      }
    } catch (error) {
      console.error('Failed to save rules:', error);
    } finally {
      setIsSaving(false);
    }
  };

  // Count active rules
  const activeRuleCount = rules.filter((r) => r.enabled).length;

  // Check if there are unsaved changes (list-level or any rule with persisted=false)
  const hasUnsavedChanges = !rulesPersistedState || rules.some((r) => !r.persisted);

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
            {hasUnsavedChanges && (
              <button
                onClick={handleSaveRules}
                disabled={isSaving}
                className={`
                  flex items-center gap-1 px-2 py-1 text-xs font-mono rounded transition-colors
                  ${isSaving
                    ? 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-600 dark:text-yellow-400 opacity-50 cursor-not-allowed'
                    : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 hover:bg-yellow-200 dark:hover:bg-yellow-900/50'
                  }
                `}
                title="Save rules to file"
              >
                <Save className={`w-3.5 h-3.5 ${isSaving ? 'animate-pulse' : ''}`} />
                <span>Save</span>
              </button>
            )}
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
          {isRulesLoading && rules.length === 0 && (
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

          {/* Rules List */}
          {!isRulesLoading && rules.length > 0 && (
            <div>
              <h3 className="text-xs font-mono text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">
                Rules ({rules.length})
              </h3>
              <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
              >
                <SortableContext
                  items={rules.map((r) => r.name)}
                  strategy={verticalListSortingStrategy}
                >
                  <div className="space-y-2">
                    {rules.map((rule, index) => (
                      <RuleItem
                        key={rule.name}
                        rule={rule}
                        index={index}
                        onToggle={handleToggleRule}
                        onEdit={handleEditRule}
                        {...(!rule.persisted && { onDelete: handleDeleteRule })}
                        isUpdating={isUpdating === rule.name}
                      />
                    ))}
                  </div>
                </SortableContext>
              </DndContext>
            </div>
          )}

          {/* Empty state */}
          {!isRulesLoading && !loadError && rules.length === 0 && (
            <div className="text-center py-8 text-gray-500 dark:text-gray-400">
              <Filter className="w-8 h-8 mx-auto mb-2 opacity-50" />
              <p className="text-sm font-mono">No rules defined</p>
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

      {/* Edit Rule Modal */}
      <EditRuleModal
        isOpen={editingRule !== null}
        rule={editingRule}
        onClose={() => setEditingRule(null)}
        onSave={handleSaveEditedRule}
      />
    </>
  );
};
