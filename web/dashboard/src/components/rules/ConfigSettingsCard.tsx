import React, { useState } from 'react';
import { Settings, Save, X } from 'lucide-react';
import type { ConfigRulesSettings, UpdateConfigRequest } from '../../api/client';

interface ConfigSettingsCardProps {
  config: ConfigRulesSettings | null;
  onUpdate: (update: UpdateConfigRequest) => Promise<void>;
  isUpdating?: boolean;
}

export const ConfigSettingsCard: React.FC<ConfigSettingsCardProps> = ({
  config,
  onUpdate,
  isUpdating = false,
}) => {
  const [isEditing, setIsEditing] = useState(false);
  const [editState, setEditState] = useState<UpdateConfigRequest>({});

  if (!config) {
    return (
      <div className="p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
        <div className="flex items-center gap-2 text-gray-500 dark:text-gray-400">
          <Settings className="w-4 h-4" />
          <span className="font-mono text-sm">No config rules loaded</span>
        </div>
      </div>
    );
  }

  const handleStartEdit = () => {
    setEditState({
      priority_min: config.priority_min,
      priority_max: config.priority_max,
      types: [...(config.types ?? [])],
      exclude_types: [...(config.exclude_types ?? [])],
      exclude_labels: [...(config.exclude_labels ?? [])],
      assignee: config.assignee,
    });
    setIsEditing(true);
  };

  const handleCancel = () => {
    setIsEditing(false);
    setEditState({});
  };

  const handleSave = async () => {
    await onUpdate(editState);
    setIsEditing(false);
    setEditState({});
  };

  const priorityDisplay =
    config.priority_max === -1
      ? `P${config.priority_min}+`
      : `P${config.priority_min}-P${config.priority_max}`;

  return (
    <div className="p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Settings className="w-4 h-4 text-gray-500 dark:text-gray-400" />
          <span className="font-mono font-normal text-sm text-gray-900 dark:text-gray-100">
            Config Settings
          </span>
        </div>
        {!isEditing ? (
          <button
            onClick={handleStartEdit}
            disabled={isUpdating}
            className="px-2 py-1 text-xs font-mono text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors disabled:opacity-50"
          >
            Edit
          </button>
        ) : (
          <div className="flex items-center gap-1">
            <button
              onClick={handleSave}
              disabled={isUpdating}
              className="p-1 text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20 rounded transition-colors disabled:opacity-50"
              title="Save changes"
            >
              <Save className="w-4 h-4" />
            </button>
            <button
              onClick={handleCancel}
              disabled={isUpdating}
              className="p-1 text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
              title="Cancel"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>

      {!isEditing ? (
        // Display mode
        <div className="space-y-2 text-sm">
          <div className="flex flex-wrap gap-2">
            <span className="px-2 py-1 bg-gray-100 dark:bg-gray-700 rounded font-mono text-xs">
              Priority: {priorityDisplay}
            </span>
            {(config.types?.length ?? 0) > 0 && (
              <span className="px-2 py-1 bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded font-mono text-xs">
                Types: {config.types.join(', ')}
              </span>
            )}
            {(config.exclude_types?.length ?? 0) > 0 && (
              <span className="px-2 py-1 bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded font-mono text-xs">
                Exclude: {config.exclude_types.join(', ')}
              </span>
            )}
            {(config.exclude_labels?.length ?? 0) > 0 && (
              <span className="px-2 py-1 bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 rounded font-mono text-xs">
                Exclude labels: {config.exclude_labels.join(', ')}
              </span>
            )}
            {config.assignee && config.assignee !== '*' && (
              <span className="px-2 py-1 bg-purple-50 dark:bg-purple-900/30 text-purple-700 dark:text-purple-400 rounded font-mono text-xs">
                Assignee: {config.assignee || 'unassigned'}
              </span>
            )}
            {config.max_concurrent > 0 && (
              <span className="px-2 py-1 bg-gray-100 dark:bg-gray-700 rounded font-mono text-xs">
                Max concurrent: {config.max_concurrent}
              </span>
            )}
          </div>
        </div>
      ) : (
        // Edit mode
        <div className="space-y-3">
          {/* Priority range */}
          <div className="flex items-center gap-2">
            <label className="text-xs font-mono text-gray-600 dark:text-gray-400 w-20">Priority:</label>
            <input
              type="number"
              min={0}
              max={4}
              value={editState.priority_min ?? config.priority_min}
              onChange={(e) =>
                setEditState((s) => ({ ...s, priority_min: parseInt(e.target.value) || 0 }))
              }
              className="w-14 px-2 py-1 text-xs font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
            />
            <span className="text-xs text-gray-500">to</span>
            <input
              type="number"
              min={-1}
              max={4}
              value={editState.priority_max ?? config.priority_max}
              onChange={(e) =>
                setEditState((s) => ({ ...s, priority_max: parseInt(e.target.value) }))
              }
              className="w-14 px-2 py-1 text-xs font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
            />
            <span className="text-xs text-gray-400">(-1 = no max)</span>
          </div>

          {/* Exclude labels */}
          <div className="flex items-start gap-2">
            <label className="text-xs font-mono text-gray-600 dark:text-gray-400 w-20 pt-1">
              Exclude labels:
            </label>
            <input
              type="text"
              value={(editState.exclude_labels ?? config.exclude_labels ?? []).join(', ')}
              onChange={(e) =>
                setEditState((s) => ({
                  ...s,
                  exclude_labels: e.target.value
                    .split(',')
                    .map((l) => l.trim())
                    .filter(Boolean),
                }))
              }
              placeholder="wip, blocked, ..."
              className="flex-1 px-2 py-1 text-xs font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
            />
          </div>

          {/* Assignee */}
          <div className="flex items-center gap-2">
            <label className="text-xs font-mono text-gray-600 dark:text-gray-400 w-20">Assignee:</label>
            <input
              type="text"
              value={editState.assignee ?? config.assignee}
              onChange={(e) => setEditState((s) => ({ ...s, assignee: e.target.value }))}
              placeholder="* for any, empty for unassigned"
              className="flex-1 px-2 py-1 text-xs font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
            />
          </div>
        </div>
      )}
    </div>
  );
};
