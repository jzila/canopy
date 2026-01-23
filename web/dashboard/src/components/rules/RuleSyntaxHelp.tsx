import React, { useState } from 'react';
import { HelpCircle, ChevronDown, ChevronUp } from 'lucide-react';

interface RuleSyntaxHelpProps {
  onInsertExample?: (value: string) => void;
}

const SYNTAX_SECTIONS = [
  {
    title: 'Fields',
    items: [
      { syntax: 'priority', description: 'Task priority (0-4, lower is higher priority)' },
      { syntax: 'type', description: 'Task type: "bug", "feature", "task", "chore"' },
      { syntax: 'labels', description: 'Array of label strings' },
      { syntax: 'assignee', description: 'Assigned user (empty string if unassigned)' },
      { syntax: 'status', description: 'Task status: "open", "in_progress", "closed"' },
    ],
  },
  {
    title: 'Operators',
    items: [
      { syntax: '==', description: 'Equal (strings need quotes)' },
      { syntax: '!=', description: 'Not equal' },
      { syntax: '>', description: 'Greater than' },
      { syntax: '<', description: 'Less than' },
      { syntax: '>=', description: 'Greater than or equal' },
      { syntax: '<=', description: 'Less than or equal' },
      { syntax: 'in', description: 'Check if value is in array' },
    ],
  },
];

const EXAMPLES = [
  { label: 'High priority', value: 'priority <= 1', description: 'Priority 0 or 1' },
  { label: 'Low priority', value: 'priority > 2', description: 'Priority 3 or 4' },
  { label: 'Bug type', value: 'type == "bug"', description: 'Only bugs' },
  { label: 'Has label', value: '"frontend" in labels', description: 'Has frontend label' },
  { label: 'Unassigned', value: 'assignee == ""', description: 'No assignee' },
  { label: 'Assigned to me', value: 'assignee == "john"', description: 'Specific assignee' },
];

export const RuleSyntaxHelp: React.FC<RuleSyntaxHelpProps> = ({ onInsertExample }) => {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center justify-between px-3 py-2 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
      >
        <div className="flex items-center gap-2 text-sm font-mono text-gray-600 dark:text-gray-400">
          <HelpCircle className="w-4 h-4" />
          <span>Syntax Help</span>
        </div>
        {isExpanded ? (
          <ChevronUp className="w-4 h-4 text-gray-400" />
        ) : (
          <ChevronDown className="w-4 h-4 text-gray-400" />
        )}
      </button>

      {isExpanded && (
        <div className="px-3 py-3 space-y-4 bg-white dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700">
          {/* Syntax reference */}
          {SYNTAX_SECTIONS.map((section) => (
            <div key={section.title}>
              <h4 className="text-xs font-mono text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">
                {section.title}
              </h4>
              <div className="space-y-1">
                {section.items.map((item) => (
                  <div key={item.syntax} className="flex items-baseline gap-2 text-xs">
                    <code className="font-mono px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded">
                      {item.syntax}
                    </code>
                    <span className="text-gray-500 dark:text-gray-400">{item.description}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}

          {/* Examples */}
          <div>
            <h4 className="text-xs font-mono text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">
              Examples {onInsertExample && '(click to insert)'}
            </h4>
            <div className="flex flex-wrap gap-1">
              {EXAMPLES.map((ex) => (
                <button
                  key={ex.value}
                  type="button"
                  onClick={() => onInsertExample?.(ex.value)}
                  disabled={!onInsertExample}
                  className={`
                    px-2 py-1 text-xs font-mono rounded transition-colors
                    ${onInsertExample
                      ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-400 hover:bg-blue-100 dark:hover:bg-blue-900/30 cursor-pointer'
                      : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 cursor-default'
                    }
                  `}
                  title={ex.description}
                >
                  {ex.label}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
