import React from 'react';
import { Power, Trash2, Settings, Terminal, Save } from 'lucide-react';
import type { RuntimeRule } from '../../api/client';

interface RuleItemProps {
  rule: RuntimeRule;
  onToggle: (name: string, enabled: boolean) => void | Promise<void>;
  onDelete?: (name: string) => void | Promise<void>;
  onPersist?: (name: string) => void | Promise<void>;
  isUpdating?: boolean;
}

export const RuleItem: React.FC<RuleItemProps> = ({
  rule,
  onToggle,
  onDelete,
  onPersist,
  isUpdating = false,
}) => {
  const isEnabled = rule.enabled !== false;
  const isConfigRule = rule.source === 'config';
  const canDelete = !isConfigRule;
  const canPersist = !isConfigRule;

  return (
    <div
      className={`
        p-4 rounded-lg border transition-all
        ${isEnabled
          ? 'bg-white dark:bg-gray-800 border-gray-200 dark:border-gray-700'
          : 'bg-gray-50 dark:bg-gray-900 border-gray-200 dark:border-gray-700 opacity-60'
        }
      `}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1 min-w-0">
          {/* Name and badges */}
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-mono font-normal text-sm text-gray-900 dark:text-gray-100">
              {rule.name}
            </span>
            <span
              className={`
                px-2 py-0.5 rounded text-xs font-mono
                ${isConfigRule
                  ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
                  : 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-400'
                }
              `}
            >
              {rule.source}
            </span>
            <span
              className={`
                px-2 py-0.5 rounded text-xs font-mono
                ${isEnabled
                  ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
                  : 'bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400'
                }
              `}
            >
              {isEnabled ? 'enabled' : 'disabled'}
            </span>
          </div>

          {/* Condition */}
          <div className="mt-2 flex items-start gap-2">
            <Terminal className="w-3.5 h-3.5 text-gray-400 dark:text-gray-500 mt-0.5 flex-shrink-0" />
            <code className="text-xs font-mono text-gray-600 dark:text-gray-400 break-all">
              {rule.action} where {rule.condition}
            </code>
          </div>

          {/* Reason */}
          {rule.reason && (
            <p className="mt-1.5 text-xs text-gray-500 dark:text-gray-400 italic">
              "{rule.reason}"
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1 flex-shrink-0">
          <button
            onClick={() => onToggle(rule.name, !isEnabled)}
            disabled={isUpdating}
            className={`
              p-1.5 rounded transition-colors
              ${isUpdating
                ? 'opacity-50 cursor-not-allowed'
                : isEnabled
                  ? 'text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20'
                  : 'text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800'
              }
            `}
            title={isEnabled ? 'Disable rule' : 'Enable rule'}
          >
            <Power className="w-4 h-4" />
          </button>

          {canPersist && onPersist && (
            <button
              onClick={() => onPersist(rule.name)}
              disabled={isUpdating}
              className={`
                p-1.5 rounded transition-colors
                ${isUpdating
                  ? 'opacity-50 cursor-not-allowed'
                  : 'text-gray-400 dark:text-gray-500 hover:text-blue-500 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20'
                }
              `}
              title="Save to config"
            >
              <Save className="w-4 h-4" />
            </button>
          )}

          {canDelete && onDelete && (
            <button
              onClick={() => onDelete(rule.name)}
              disabled={isUpdating}
              className={`
                p-1.5 rounded transition-colors
                ${isUpdating
                  ? 'opacity-50 cursor-not-allowed'
                  : 'text-gray-400 dark:text-gray-500 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20'
                }
              `}
              title="Delete rule"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          )}

          {isConfigRule && (
            <span
              className="p-1.5 text-gray-300 dark:text-gray-600"
              title="Config rules cannot be deleted, only disabled"
            >
              <Settings className="w-4 h-4" />
            </span>
          )}
        </div>
      </div>
    </div>
  );
};
