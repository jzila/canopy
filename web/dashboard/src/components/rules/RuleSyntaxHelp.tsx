import React, { useState } from 'react';
import { ChevronDown, ChevronRight, HelpCircle } from 'lucide-react';

interface RuleSyntaxHelpProps {
  defaultExpanded?: boolean;
}

const FIELDS = [
  { name: 'priority', type: 'number', description: '0-4 (0=critical, 4=backlog)', operators: '==, !=, <, >, <=, >=' },
  { name: 'type', type: 'string', description: 'bug, feature, task, chore', operators: '==, !=' },
  { name: 'status', type: 'string', description: 'open, in_progress, closed, deferred', operators: '==, !=' },
  { name: 'assignee', type: 'string', description: 'username or empty string', operators: '==, !=' },
  { name: 'labels', type: 'array', description: 'list of string labels', operators: 'in, not in' },
  { name: 'title', type: 'string', description: 'task title', operators: 'contains' },
  { name: 'description', type: 'string', description: 'task description', operators: 'contains' },
  { name: 'id', type: 'string', description: 'task ID (e.g., beads-abc)', operators: '==, !=, contains' },
];

const EXAMPLES = [
  { condition: 'priority > 2', description: 'Low priority tasks (P3-P4)' },
  { condition: 'type == "bug"', description: 'Bug tasks only' },
  { condition: 'assignee == ""', description: 'Unassigned tasks' },
  { condition: '"frontend" in labels', description: 'Tasks with frontend label' },
  { condition: '"wip" not in labels', description: 'Tasks without wip label' },
  { condition: 'title contains "fix"', description: 'Tasks with "fix" in title' },
  { condition: 'priority <= 1 and type == "bug"', description: 'High priority bugs' },
  { condition: 'type == "bug" or type == "feature"', description: 'Bugs or features' },
];

export const RuleSyntaxHelp: React.FC<RuleSyntaxHelpProps> = ({
  defaultExpanded = false,
}) => {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
      >
        {isExpanded ? (
          <ChevronDown className="w-4 h-4 text-gray-500 dark:text-gray-400" />
        ) : (
          <ChevronRight className="w-4 h-4 text-gray-500 dark:text-gray-400" />
        )}
        <HelpCircle className="w-4 h-4 text-blue-500 dark:text-blue-400" />
        <span className="text-xs font-mono text-gray-700 dark:text-gray-300">
          Syntax Help
        </span>
      </button>

      {isExpanded && (
        <div className="px-3 py-3 space-y-4 bg-white dark:bg-gray-900 text-xs">
          {/* Available Fields */}
          <div>
            <h4 className="font-mono text-gray-900 dark:text-gray-100 mb-2">
              Available Fields
            </h4>
            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700">
                    <th className="pb-1 pr-3 font-mono text-gray-500 dark:text-gray-400">Field</th>
                    <th className="pb-1 pr-3 font-mono text-gray-500 dark:text-gray-400">Type</th>
                    <th className="pb-1 pr-3 font-mono text-gray-500 dark:text-gray-400">Operators</th>
                    <th className="pb-1 font-mono text-gray-500 dark:text-gray-400">Values</th>
                  </tr>
                </thead>
                <tbody className="font-mono">
                  {FIELDS.map((field) => (
                    <tr key={field.name} className="border-b border-gray-100 dark:border-gray-800 last:border-0">
                      <td className="py-1 pr-3 text-blue-600 dark:text-blue-400">{field.name}</td>
                      <td className="py-1 pr-3 text-gray-500 dark:text-gray-400">{field.type}</td>
                      <td className="py-1 pr-3 text-gray-600 dark:text-gray-300">{field.operators}</td>
                      <td className="py-1 text-gray-500 dark:text-gray-400">{field.description}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Operators */}
          <div>
            <h4 className="font-mono text-gray-900 dark:text-gray-100 mb-2">
              Logical Operators
            </h4>
            <div className="space-y-1 font-mono text-gray-600 dark:text-gray-300">
              <div><code className="text-purple-600 dark:text-purple-400">and</code> - both conditions must be true</div>
              <div><code className="text-purple-600 dark:text-purple-400">or</code> - either condition must be true</div>
            </div>
          </div>

          {/* Examples */}
          <div>
            <h4 className="font-mono text-gray-900 dark:text-gray-100 mb-2">
              Examples
            </h4>
            <div className="space-y-1.5">
              {EXAMPLES.map((ex, i) => (
                <div key={i} className="flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-2">
                  <code className="font-mono text-green-600 dark:text-green-400 break-all">
                    {ex.condition}
                  </code>
                  <span className="text-gray-500 dark:text-gray-400">
                    — {ex.description}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* Notes */}
          <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
            <p className="text-gray-500 dark:text-gray-400">
              String values must be quoted with single or double quotes.
              Multiple conditions can be separated by commas or newlines.
            </p>
          </div>
        </div>
      )}
    </div>
  );
};
