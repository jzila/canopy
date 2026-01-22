import React, { useState, useRef, useEffect } from 'react';
import { X, Plus, Trash2, AlertCircle, Save } from 'lucide-react';
import type { Rule } from '../../api/client';
import { RuleSyntaxHelp } from './RuleSyntaxHelp';

interface EditRuleModalProps {
  isOpen: boolean;
  rule: Rule | null;
  onClose: () => void;
  onSave: (rule: Rule) => void;
}

interface ConditionRow {
  id: string;
  field: string;
  operator: string;
  value: string;
}

const FIELD_OPTIONS = [
  { value: 'priority', label: 'priority' },
  { value: 'type', label: 'type' },
  { value: 'assignee', label: 'assignee' },
  { value: 'status', label: 'status' },
  { value: 'labels', label: 'labels' },
];

const OPERATOR_OPTIONS = [
  { value: '==', label: '==' },
  { value: '!=', label: '!=' },
  { value: '>', label: '>' },
  { value: '<', label: '<' },
  { value: '>=', label: '>=' },
  { value: '<=', label: '<=' },
  { value: 'in', label: 'in' },
];

// Parse a condition string into field/operator/value
function parseCondition(condition: string): ConditionRow {
  const id = crypto.randomUUID();

  // Handle "value" in field pattern (e.g., "frontend" in labels)
  const inMatch = condition.match(/^"([^"]+)"\s+in\s+(\w+)$/);
  if (inMatch) {
    return { id, field: inMatch[2]!, operator: 'in', value: inMatch[1]! };
  }

  // Handle field operator value pattern
  const match = condition.match(/^(\w+)\s*(==|!=|>=|<=|>|<)\s*(.+)$/);
  if (match) {
    let value = match[3]!.trim();
    // Remove quotes from string values
    if (value.startsWith('"') && value.endsWith('"')) {
      value = value.slice(1, -1);
    }
    return { id, field: match[1]!, operator: match[2]!, value };
  }

  // Fallback: treat entire condition as a raw value
  return { id, field: '', operator: '==', value: condition };
}

// Convert a condition row back to a string
function conditionToString(row: ConditionRow): string {
  if (!row.field || !row.value) return '';

  // Handle "in" operator specially for labels
  if (row.operator === 'in') {
    return `"${row.value}" in ${row.field}`;
  }

  // Quote string values (non-numeric)
  const needsQuotes = isNaN(Number(row.value));
  const quotedValue = needsQuotes ? `"${row.value}"` : row.value;

  return `${row.field} ${row.operator} ${quotedValue}`;
}

export const EditRuleModal: React.FC<EditRuleModalProps> = ({
  isOpen,
  rule,
  onClose,
  onSave,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null);
  const [conditions, setConditions] = useState<ConditionRow[]>([]);
  const [action, setAction] = useState<'deny' | 'allow'>('deny');
  const [enabled, setEnabled] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Initialize form when rule changes or dialog opens
  useEffect(() => {
    if (isOpen && rule) {
      const parsedConditions = rule.conditions.length > 0
        ? rule.conditions.map(parseCondition)
        : [{ id: crypto.randomUUID(), field: '', operator: '==', value: '' }];
      setConditions(parsedConditions);
      setAction(rule.action);
      setEnabled(rule.enabled);
      setError(null);
    }
  }, [isOpen, rule]);

  // Escape key handling
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, onClose]);

  // Click outside handling
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dialogRef.current && !dialogRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen, onClose]);

  const handleAddCondition = () => {
    setConditions([
      ...conditions,
      { id: crypto.randomUUID(), field: '', operator: '==', value: '' },
    ]);
  };

  const handleRemoveCondition = (id: string) => {
    if (conditions.length > 1) {
      setConditions(conditions.filter((c) => c.id !== id));
    }
  };

  const handleConditionChange = (
    id: string,
    field: keyof Omit<ConditionRow, 'id'>,
    value: string
  ) => {
    setConditions(
      conditions.map((c) => (c.id === id ? { ...c, [field]: value } : c))
    );
  };

  const handleInsertExample = (example: string) => {
    const parsed = parseCondition(example);
    // Add to first empty row or append
    const emptyIndex = conditions.findIndex((c) => !c.field && !c.value);
    if (emptyIndex >= 0) {
      setConditions(
        conditions.map((c, i) => (i === emptyIndex ? parsed : c))
      );
    } else {
      setConditions([...conditions, parsed]);
    }
  };

  const handleSave = () => {
    if (!rule) return;
    setError(null);

    // Filter out empty conditions and convert to strings
    const validConditions = conditions
      .filter((c) => c.field && c.value)
      .map(conditionToString)
      .filter((s) => s.length > 0);

    if (validConditions.length === 0) {
      setError('At least one valid condition is required');
      return;
    }

    // Create updated rule with persisted=false since this is an in-memory edit
    const updatedRule: Rule = {
      name: rule.name,
      conditions: validConditions,
      action,
      enabled,
      persisted: false,
    };

    onSave(updatedRule);
    onClose();
  };

  if (!isOpen || !rule) return null;

  const hasValidConditions = conditions.some((c) => c.field && c.value);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        ref={dialogRef}
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[90vh] flex flex-col"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
          <h2 className="font-mono font-normal text-lg text-gray-900 dark:text-gray-100">
            Edit Rule
          </h2>
          <button
            onClick={onClose}
            className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Form */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Error message */}
          {error && (
            <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm">
              <AlertCircle className="w-4 h-4 flex-shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Name (read-only) */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-1">
              Name
            </label>
            <div className="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-gray-700 rounded-lg bg-gray-50 dark:bg-gray-900 text-gray-700 dark:text-gray-300">
              {rule.name}
            </div>
          </div>

          {/* Conditions */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="block text-xs font-mono text-gray-600 dark:text-gray-400">
                Conditions
              </label>
              <button
                type="button"
                onClick={handleAddCondition}
                className="flex items-center gap-1 px-2 py-1 text-xs font-mono text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
              >
                <Plus className="w-3 h-3" />
                Add
              </button>
            </div>
            <div className="space-y-2">
              {conditions.map((condition, index) => (
                <div key={condition.id} className="flex items-center gap-2">
                  {/* Field selector */}
                  <select
                    value={condition.field}
                    onChange={(e) =>
                      handleConditionChange(condition.id, 'field', e.target.value)
                    }
                    className="flex-1 px-2 py-1.5 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="">field</option>
                    {FIELD_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>

                  {/* Operator selector */}
                  <select
                    value={condition.operator}
                    onChange={(e) =>
                      handleConditionChange(condition.id, 'operator', e.target.value)
                    }
                    className="w-16 px-2 py-1.5 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    {OPERATOR_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>

                  {/* Value input */}
                  <input
                    type="text"
                    value={condition.value}
                    onChange={(e) =>
                      handleConditionChange(condition.id, 'value', e.target.value)
                    }
                    placeholder="value"
                    className="flex-1 px-2 py-1.5 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />

                  {/* Remove button */}
                  <button
                    type="button"
                    onClick={() => handleRemoveCondition(condition.id)}
                    disabled={conditions.length <= 1}
                    className={`
                      p-1.5 rounded transition-colors
                      ${conditions.length <= 1
                        ? 'text-gray-300 dark:text-gray-600 cursor-not-allowed'
                        : 'text-gray-400 dark:text-gray-500 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20'
                      }
                    `}
                    title={conditions.length <= 1 ? 'At least one condition required' : 'Remove condition'}
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              ))}
            </div>
          </div>

          {/* Syntax Help */}
          <RuleSyntaxHelp onInsertExample={handleInsertExample} />

          {/* Action (Radio buttons) */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-2">
              Action
            </label>
            <div className="flex gap-4">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="action"
                  value="deny"
                  checked={action === 'deny'}
                  onChange={() => setAction('deny')}
                  className="w-4 h-4 text-red-600 border-gray-300 dark:border-gray-600 focus:ring-red-500"
                />
                <span className="text-sm font-mono text-gray-700 dark:text-gray-300">
                  DENY
                </span>
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  Deny matching tasks
                </span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="action"
                  value="allow"
                  checked={action === 'allow'}
                  onChange={() => setAction('allow')}
                  className="w-4 h-4 text-green-600 border-gray-300 dark:border-gray-600 focus:ring-green-500"
                />
                <span className="text-sm font-mono text-gray-700 dark:text-gray-300">
                  ALLOW
                </span>
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  Allow matching tasks
                </span>
              </label>
            </div>
          </div>

          {/* Enabled checkbox */}
          <div>
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
                className="w-4 h-4 text-blue-600 border-gray-300 dark:border-gray-600 rounded focus:ring-blue-500"
              />
              <div>
                <span className="text-sm font-mono text-gray-700 dark:text-gray-300">
                  Enabled
                </span>
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Rule will be applied when enabled
                </p>
              </div>
            </label>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 px-4 py-3 border-t border-gray-200 dark:border-gray-700 flex-shrink-0">
          <button
            type="button"
            onClick={handleSave}
            disabled={!hasValidConditions}
            className={`
              flex items-center gap-2 px-4 py-2 text-sm font-mono text-white rounded-lg transition-colors
              ${!hasValidConditions
                ? 'bg-blue-400 cursor-not-allowed'
                : 'bg-blue-500 hover:bg-blue-600'
              }
            `}
          >
            <Save className="w-4 h-4" />
            Save
          </button>
        </div>
      </div>
    </div>
  );
};
