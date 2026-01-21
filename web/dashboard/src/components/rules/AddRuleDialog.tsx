import React, { useState, useRef, useEffect } from 'react';
import { X, Plus, AlertCircle } from 'lucide-react';
import type { AddRuleRequest } from '../../api/client';

interface AddRuleDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (rule: AddRuleRequest) => Promise<{ success: boolean; error?: string }>;
  isAdding?: boolean;
}

const CONDITION_EXAMPLES = [
  { label: 'Priority filter', value: 'priority > 2' },
  { label: 'Type filter', value: 'type == "bug"' },
  { label: 'Label check', value: '"frontend" in labels' },
  { label: 'Assignee filter', value: 'assignee == ""' },
];

const ACTION_OPTIONS = [
  { value: 'skip', label: 'Skip', description: 'Skip matching tasks' },
  { value: 'include', label: 'Include', description: 'Force include matching tasks' },
];

export const AddRuleDialog: React.FC<AddRuleDialogProps> = ({
  isOpen,
  onClose,
  onAdd,
  isAdding = false,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState('');
  const [condition, setCondition] = useState('');
  const [action, setAction] = useState('skip');
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);

  // Reset form when dialog opens
  useEffect(() => {
    if (isOpen) {
      setName('');
      setCondition('');
      setAction('skip');
      setReason('');
      setError(null);
    }
  }, [isOpen]);

  // Escape key handling
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen && !isAdding) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, isAdding, onClose]);

  // Click outside handling
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dialogRef.current && !dialogRef.current.contains(e.target as Node) && !isAdding) {
        onClose();
      }
    };
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen, isAdding, onClose]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (!condition.trim()) {
      setError('Condition is required');
      return;
    }

    const request: AddRuleRequest = {
      name: name.trim(),
      condition: condition.trim(),
      action,
    };
    if (reason.trim()) {
      request.reason = reason.trim();
    }
    const result = await onAdd(request);

    if (!result.success) {
      setError(result.error || 'Failed to add rule');
    } else {
      onClose();
    }
  };

  const handleConditionExample = (value: string) => {
    setCondition(value);
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        ref={dialogRef}
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md mx-4"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
          <h2 className="font-mono font-normal text-lg text-gray-900 dark:text-gray-100">
            Add Rule
          </h2>
          <button
            onClick={onClose}
            disabled={isAdding}
            className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="p-4 space-y-4">
          {/* Error message */}
          {error && (
            <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm">
              <AlertCircle className="w-4 h-4 flex-shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Name */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-1">
              Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="incident-mode"
              className="w-full px-3 py-2 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              disabled={isAdding}
            />
          </div>

          {/* Condition */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-1">
              Condition
            </label>
            <input
              type="text"
              value={condition}
              onChange={(e) => setCondition(e.target.value)}
              placeholder="priority > 2"
              className="w-full px-3 py-2 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              disabled={isAdding}
            />
            {/* Examples */}
            <div className="mt-2 flex flex-wrap gap-1">
              {CONDITION_EXAMPLES.map((ex) => (
                <button
                  key={ex.value}
                  type="button"
                  onClick={() => handleConditionExample(ex.value)}
                  className="px-2 py-0.5 text-xs font-mono bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 rounded hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
                  disabled={isAdding}
                >
                  {ex.label}
                </button>
              ))}
            </div>
          </div>

          {/* Action */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-1">
              Action
            </label>
            <div className="flex gap-2">
              {ACTION_OPTIONS.map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setAction(opt.value)}
                  disabled={isAdding}
                  className={`
                    flex-1 px-3 py-2 text-sm font-mono rounded-lg border transition-colors
                    ${action === opt.value
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
                      : 'border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:border-gray-400 dark:hover:border-gray-500'
                    }
                    disabled:opacity-50
                  `}
                >
                  <div className="font-medium">{opt.label}</div>
                  <div className="text-xs opacity-70">{opt.description}</div>
                </button>
              ))}
            </div>
          </div>

          {/* Reason */}
          <div>
            <label className="block text-xs font-mono text-gray-600 dark:text-gray-400 mb-1">
              Reason (optional)
            </label>
            <input
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Incident mode - P0/P1 only"
              className="w-full px-3 py-2 text-sm font-mono border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              disabled={isAdding}
            />
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              disabled={isAdding}
              className="px-4 py-2 text-sm font-mono text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isAdding || !name.trim() || !condition.trim()}
              className={`
                flex items-center gap-2 px-4 py-2 text-sm font-mono text-white rounded-lg transition-colors
                ${isAdding || !name.trim() || !condition.trim()
                  ? 'bg-blue-400 cursor-not-allowed'
                  : 'bg-blue-500 hover:bg-blue-600'
                }
              `}
            >
              <Plus className="w-4 h-4" />
              Add Rule
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
