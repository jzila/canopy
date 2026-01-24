import React, { useState } from 'react';
import { List, Terminal as TerminalIcon, GitCommit, GripHorizontal, Info } from 'lucide-react';
import { AgentTerminal } from '../agents/AgentTerminal';
import { LiveFeed } from '../agents/LiveFeed';
import { CommitList } from '../agents/CommitList';
import { AgentDetail } from '../agents/AgentDetail';
import type { AgentState } from '../../stores/stateStore';

type TerminalTab = 'feed' | 'terminal' | 'commits' | 'detail';

export interface TerminalPanelProps {
  /** The selected agent to display */
  agent: AgentState;
  /** Height of the panel in pixels */
  height: number;
  /** Whether the panel is currently being resized */
  isResizing: boolean;
  /** Handler for starting resize drag */
  onResizeStart: (e: React.MouseEvent) => void;
  /** Called when close button is clicked */
  onClose: () => void;
  /** Called when a different agent is selected (e.g., clicking on child agent) */
  onSelectAgent?: (agentId: string) => void;
}

/**
 * Terminal panel component for displaying agent output.
 *
 * Contains:
 * - Resize handle
 * - Tab navigation (Live Feed, Raw Output, Commits)
 * - Agent info display
 * - Close button
 */
export const TerminalPanel: React.FC<TerminalPanelProps> = ({
  agent,
  height,
  isResizing,
  onResizeStart,
  onClose,
  onSelectAgent,
}) => {
  const [activeTab, setActiveTab] = useState<TerminalTab>('feed');

  return (
    <div
      className="border-t border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 flex-shrink-0"
      style={{ height }}
    >
      <div className="h-full flex flex-col">
        {/* Resize Handle */}
        <div
          onMouseDown={onResizeStart}
          className={`
            h-1.5 bg-gray-200 dark:bg-gray-800 cursor-ns-resize flex items-center justify-center
            hover:bg-gray-300 dark:hover:bg-gray-700 transition-colors group
            ${isResizing ? 'bg-blue-600' : ''}
          `}
          title="Drag to resize"
        >
          <GripHorizontal
            className={`w-4 h-4 text-gray-400 dark:text-gray-600 group-hover:text-gray-500 dark:group-hover:text-gray-400 ${
              isResizing ? 'text-blue-400' : ''
            }`}
          />
        </div>

        {/* Terminal Header with Tabs */}
        <div className="flex items-center justify-between px-4 py-2 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-4">
            {/* Tab buttons */}
            <div className="flex items-center gap-1 bg-gray-200/50 dark:bg-gray-900/50 rounded-lg p-1">
              <button
                onClick={() => setActiveTab('feed')}
                className={`
                  flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                  ${
                    activeTab === 'feed'
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-300/50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <List className="w-4 h-4" />
                Live Feed
              </button>
              <button
                onClick={() => setActiveTab('terminal')}
                className={`
                  flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                  ${
                    activeTab === 'terminal'
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-300/50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <TerminalIcon className="w-4 h-4" />
                Raw Output
              </button>
              <button
                onClick={() => setActiveTab('commits')}
                className={`
                  flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                  ${
                    activeTab === 'commits'
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-300/50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <GitCommit className="w-4 h-4" />
                Commits
                {(agent.commits ?? 0) > 0 && (
                  <span className="ml-1 px-1.5 py-0.5 text-xs bg-blue-500/30 rounded">
                    {agent.commits}
                  </span>
                )}
              </button>
              <button
                onClick={() => setActiveTab('detail')}
                className={`
                  flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-all
                  ${
                    activeTab === 'detail'
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-300/50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <Info className="w-4 h-4" />
                Detail
              </button>
            </div>

            {/* Agent info */}
            <div className="flex items-center gap-2 text-xs">
              <span className="text-gray-400 dark:text-gray-500">|</span>
              <code className="font-mono text-gray-600 dark:text-gray-400">{agent.id}</code>
              <span className="text-gray-400 dark:text-gray-500">-</span>
              <span className="text-gray-600 dark:text-gray-400 truncate max-w-[200px]">{agent.task_title}</span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 transition-colors px-2 py-1 rounded hover:bg-gray-200 dark:hover:bg-gray-700"
            >
              <span className="text-sm">Close</span>
            </button>
          </div>
        </div>

        {/* Tab Content */}
        <div className="flex-1 overflow-hidden">
          {activeTab === 'feed' && <LiveFeed agentId={agent.id} />}
          {activeTab === 'terminal' && <AgentTerminal agentId={agent.id} />}
          {activeTab === 'commits' && (
            <div className="h-full overflow-y-auto p-4 bg-gray-50 dark:bg-gray-900">
              <CommitList commits={agent.git_commits || []} />
            </div>
          )}
          {activeTab === 'detail' && <AgentDetail agent={agent} {...(onSelectAgent && { onSelectAgent })} />}
        </div>
      </div>
    </div>
  );
};
