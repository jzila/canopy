import React from 'react';
import { Power, Trash2, Pencil, Terminal, GripVertical } from 'lucide-react';
import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import type { Rule } from '../../api/client';

interface RuleItemProps {
  rule: Rule;
  index: number;
  onToggle: (name: string, enabled: boolean) => void | Promise<void>;
  onEdit?: (rule: Rule) => void;
  onDelete?: (name: string) => void | Promise<void>;
  isUpdating?: boolean;
}

export const RuleItem: React.FC<RuleItemProps> = ({
  rule,
  index,
  onToggle,
  onEdit,
  onDelete,
  isUpdating = false,
}) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: rule.name });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const isEnabled = rule.enabled;
  const isPersisted = rule.persisted;
  const canDelete = !isPersisted;

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`
        p-4 rounded-lg border transition-all
        ${isDragging
          ? 'opacity-50 shadow-lg z-50'
          : ''
        }
        ${isEnabled
          ? 'bg-white dark:bg-gray-800 border-gray-200 dark:border-gray-700'
          : 'bg-gray-50 dark:bg-gray-900 border-gray-200 dark:border-gray-700 opacity-60'
        }
      `}
    >
      <div className="flex items-start justify-between gap-3">
        {/* Drag handle */}
        <button
          {...attributes}
          {...listeners}
          className="p-1 -ml-1 cursor-grab text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 active:cursor-grabbing"
          title="Drag to reorder"
        >
          <GripVertical className="w-4 h-4" />
        </button>

        <div className="flex-1 min-w-0">
          {/* Name and badges */}
          <div className="flex items-center gap-2 flex-wrap">
            {/* Order number */}
            <span className="inline-flex items-center justify-center w-5 h-5 text-xs font-mono bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 rounded">
              {index + 1}
            </span>
            {/* Yellow dot for unsaved changes */}
            {!isPersisted && (
              <span
                className="w-2 h-2 rounded-full bg-yellow-400"
                title="Unsaved changes"
              />
            )}
            <span className="font-mono font-normal text-sm text-gray-900 dark:text-gray-100">
              {rule.name}
            </span>
            <span
              className={`
                px-2 py-0.5 rounded text-xs font-mono
                ${rule.action === 'deny'
                  ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400'
                  : 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
                }
              `}
            >
              {rule.action}
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

          {/* Conditions */}
          <div className="mt-2 flex items-start gap-2">
            <Terminal className="w-3.5 h-3.5 text-gray-400 dark:text-gray-500 mt-0.5 flex-shrink-0" />
            <code className="text-xs font-mono text-gray-600 dark:text-gray-400 break-all">
              {rule.condition || '(always)'}
            </code>
          </div>
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

          {onEdit && (
            <button
              onClick={() => onEdit(rule)}
              disabled={isUpdating}
              className={`
                p-1.5 rounded transition-colors
                ${isUpdating
                  ? 'opacity-50 cursor-not-allowed'
                  : 'text-gray-400 dark:text-gray-500 hover:text-blue-500 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20'
                }
              `}
              title="Edit rule"
            >
              <Pencil className="w-4 h-4" />
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
        </div>
      </div>
    </div>
  );
};
