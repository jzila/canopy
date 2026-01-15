import React, { useEffect, useRef } from 'react';
import {
  FileText,
  Terminal,
  Search,
  FolderSearch,
  Edit3,
  Download,
  GitCommit,
  MessageSquare,
  CheckCircle,
  XCircle,
  Clock,
  ChevronRight,
  History,
} from 'lucide-react';
import { useStateStore } from '../../stores/stateStore';

interface LiveFeedProps {
  agentId: string;
}

export interface LiveFeedEvent {
  id: string;
  timestamp: string;
  event_type: 'tool_use' | 'file_change' | 'text' | 'tool_result' | 'error' | 'agent_completed';
  data: Record<string, unknown>;
}

// Tool icon mapping
const getToolIcon = (toolName: string): React.ReactNode => {
  const iconClass = 'w-4 h-4';
  switch (toolName) {
    case 'Read':
      return <FileText className={iconClass} />;
    case 'Write':
      return <Download className={iconClass} />;
    case 'Edit':
      return <Edit3 className={iconClass} />;
    case 'Bash':
      return <Terminal className={iconClass} />;
    case 'Grep':
      return <Search className={iconClass} />;
    case 'Glob':
      return <FolderSearch className={iconClass} />;
    case 'Task':
      return <GitCommit className={iconClass} />;
    default:
      return <ChevronRight className={iconClass} />;
  }
};

// Event type colors
const eventTypeStyles: Record<string, { bg: string; border: string; text: string; icon: string }> = {
  tool_use: {
    bg: 'bg-blue-900/30',
    border: 'border-blue-500/50',
    text: 'text-blue-300',
    icon: 'text-blue-400',
  },
  file_change: {
    bg: 'bg-purple-900/30',
    border: 'border-purple-500/50',
    text: 'text-purple-300',
    icon: 'text-purple-400',
  },
  text: {
    bg: 'bg-gray-800/50',
    border: 'border-gray-600/50',
    text: 'text-gray-200',
    icon: 'text-gray-400',
  },
  tool_result: {
    bg: 'bg-green-900/30',
    border: 'border-green-500/50',
    text: 'text-green-300',
    icon: 'text-green-400',
  },
  error: {
    bg: 'bg-red-900/30',
    border: 'border-red-500/50',
    text: 'text-red-300',
    icon: 'text-red-400',
  },
  agent_completed: {
    bg: 'bg-emerald-900/30',
    border: 'border-emerald-500/50',
    text: 'text-emerald-300',
    icon: 'text-emerald-400',
  },
};

// Historic event styles (muted versions)
const historicEventStyles: Record<string, { bg: string; border: string; text: string; icon: string }> = {
  text: {
    bg: 'bg-amber-900/20',
    border: 'border-amber-500/30',
    text: 'text-amber-200',
    icon: 'text-amber-400',
  },
  agent_completed: {
    bg: 'bg-emerald-900/20',
    border: 'border-emerald-500/30',
    text: 'text-emerald-200',
    icon: 'text-emerald-400',
  },
};

// Format timestamp for display
const formatTime = (timestamp: string): string => {
  try {
    const date = new Date(timestamp);
    return date.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    });
  } catch {
    return '--:--:--';
  }
};

// Truncate long strings
const truncate = (str: string, maxLen: number): string => {
  if (str.length <= maxLen) return str;
  return str.slice(0, maxLen - 3) + '...';
};

// Extract filename from path
const getFileName = (path: string): string => {
  const parts = path.split('/');
  return parts[parts.length - 1] || path;
};

// Render tool use event
const renderToolUse = (data: Record<string, unknown>) => {
  const tool = data.tool as string;
  const filePath = data.file_path as string | undefined;
  const command = data.command as string | undefined;
  const pattern = data.pattern as string | undefined;
  const description = data.description as string | undefined;

  return (
    <div className="flex items-start gap-3">
      <div className="flex-shrink-0 mt-0.5 text-blue-400">
        {getToolIcon(tool)}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <span className="font-semibold text-blue-300">{tool}</span>
          {description && (
            <span className="text-xs text-gray-400">- {truncate(description, 50)}</span>
          )}
        </div>
        {filePath && (
          <div className="flex items-center gap-1 text-sm">
            <span className="text-gray-500">File:</span>
            <code className="text-yellow-300 bg-yellow-900/20 px-1.5 py-0.5 rounded text-xs font-mono">
              {getFileName(filePath)}
            </code>
            <span className="text-gray-600 text-xs truncate" title={filePath}>
              {filePath !== getFileName(filePath) && `(${truncate(filePath, 40)})`}
            </span>
          </div>
        )}
        {command && (
          <div className="mt-1">
            <code className="text-green-300 bg-green-900/20 px-2 py-1 rounded text-xs font-mono block truncate">
              $ {truncate(command, 80)}
            </code>
          </div>
        )}
        {pattern && (
          <div className="flex items-center gap-1 text-sm">
            <span className="text-gray-500">Pattern:</span>
            <code className="text-cyan-300 bg-cyan-900/20 px-1.5 py-0.5 rounded text-xs font-mono">
              {truncate(pattern, 40)}
            </code>
          </div>
        )}
      </div>
    </div>
  );
};

// Render file change event
const renderFileChange = (data: Record<string, unknown>) => {
  const action = data.action as string | undefined;
  const filePath = data.file_path as string | undefined;
  const linesAdded = data.lines_added as number | undefined;
  const linesRemoved = data.lines_removed as number | undefined;

  return (
    <div className="flex items-start gap-3">
      <div className="flex-shrink-0 mt-0.5 text-purple-400">
        <Edit3 className="w-4 h-4" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <span className="font-semibold text-purple-300">
            {action || 'Modified'}
          </span>
          {filePath && (
            <code className="text-yellow-300 bg-yellow-900/20 px-1.5 py-0.5 rounded text-xs font-mono">
              {getFileName(filePath)}
            </code>
          )}
        </div>
        {(linesAdded !== undefined || linesRemoved !== undefined) && (
          <div className="flex items-center gap-3 text-xs">
            {linesAdded !== undefined && linesAdded > 0 && (
              <span className="text-green-400">+{linesAdded}</span>
            )}
            {linesRemoved !== undefined && linesRemoved > 0 && (
              <span className="text-red-400">-{linesRemoved}</span>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

// Render assistant text
const renderText = (data: Record<string, unknown>) => {
  const text = data.text as string | undefined;
  if (!text) return null;

  // Truncate very long text
  const displayText = truncate(text, 300);

  return (
    <div className="flex items-start gap-3">
      <div className="flex-shrink-0 mt-0.5 text-gray-400">
        <MessageSquare className="w-4 h-4" />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-gray-200 text-sm whitespace-pre-wrap break-words">
          {displayText}
        </p>
      </div>
    </div>
  );
};

// Render tool result
const renderToolResult = (data: Record<string, unknown>) => {
  const success = data.success as boolean | undefined;
  const tool = data.tool as string | undefined;
  const summary = data.summary as string | undefined;

  return (
    <div className="flex items-start gap-3">
      <div className={`flex-shrink-0 mt-0.5 ${success !== false ? 'text-green-400' : 'text-red-400'}`}>
        {success !== false ? (
          <CheckCircle className="w-4 h-4" />
        ) : (
          <XCircle className="w-4 h-4" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`font-semibold ${success !== false ? 'text-green-300' : 'text-red-300'}`}>
            {tool ? `${tool} ` : ''}
            {success !== false ? 'completed' : 'failed'}
          </span>
        </div>
        {summary && (
          <p className="text-gray-400 text-sm mt-1">{truncate(summary, 100)}</p>
        )}
      </div>
    </div>
  );
};

// Render error event
const renderError = (data: Record<string, unknown>) => {
  const message = (data.message || data.error) as string | undefined;

  return (
    <div className="flex items-start gap-3">
      <div className="flex-shrink-0 mt-0.5 text-red-400">
        <XCircle className="w-4 h-4" />
      </div>
      <div className="flex-1 min-w-0">
        <span className="font-semibold text-red-300">Error</span>
        {message && (
          <p className="text-red-200 text-sm mt-1">{truncate(message, 200)}</p>
        )}
      </div>
    </div>
  );
};

// Render agent completed event
const renderAgentCompleted = (data: Record<string, unknown>) => {
  const resultMessage = data.result_message as string | undefined;
  const filesChanged = data.files_changed as number | undefined;
  const commitsCreated = data.commits_created as number | undefined;
  const error = data.error as string | undefined;

  return (
    <div className="flex items-start gap-3">
      <div className={`flex-shrink-0 mt-0.5 ${error ? 'text-red-400' : 'text-emerald-400'}`}>
        {error ? <XCircle className="w-4 h-4" /> : <CheckCircle className="w-4 h-4" />}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-2">
          <span className={`font-semibold ${error ? 'text-red-300' : 'text-emerald-300'}`}>
            {error ? 'Agent Failed' : 'Agent Completed'}
          </span>
          {(filesChanged !== undefined && filesChanged > 0) && (
            <span className="text-xs bg-purple-900/30 text-purple-300 px-2 py-0.5 rounded">
              {filesChanged} file{filesChanged !== 1 ? 's' : ''} changed
            </span>
          )}
          {(commitsCreated !== undefined && commitsCreated > 0) && (
            <span className="text-xs bg-blue-900/30 text-blue-300 px-2 py-0.5 rounded">
              {commitsCreated} commit{commitsCreated !== 1 ? 's' : ''}
            </span>
          )}
        </div>
        {error && (
          <p className="text-red-200 text-sm mb-2">{truncate(error, 200)}</p>
        )}
        {resultMessage && (
          <div className="text-gray-200 text-sm whitespace-pre-wrap break-words bg-gray-800/50 rounded p-3 border border-gray-700/50">
            {resultMessage}
          </div>
        )}
      </div>
    </div>
  );
};

// Default styles for unknown event types
const defaultStyles = {
  bg: 'bg-gray-800/50',
  border: 'border-gray-600/50',
  text: 'text-gray-200',
  icon: 'text-gray-400',
};

// Single event component
const FeedEvent: React.FC<{ event: LiveFeedEvent }> = ({ event }) => {
  const isHistoric = event.data.is_historic === true;

  // Use historic styles if available, otherwise fall back to regular styles
  const styles = isHistoric
    ? (historicEventStyles[event.event_type] ?? eventTypeStyles[event.event_type] ?? defaultStyles)
    : (eventTypeStyles[event.event_type] ?? defaultStyles);

  const renderContent = () => {
    switch (event.event_type) {
      case 'tool_use':
        return renderToolUse(event.data);
      case 'file_change':
        return renderFileChange(event.data);
      case 'text':
        return renderText(event.data);
      case 'tool_result':
        return renderToolResult(event.data);
      case 'error':
        return renderError(event.data);
      case 'agent_completed':
        return renderAgentCompleted(event.data);
      default:
        return (
          <pre className="text-xs text-gray-400 overflow-auto">
            {JSON.stringify(event.data, null, 2)}
          </pre>
        );
    }
  };

  return (
    <div
      className={`
        ${styles.bg} ${styles.border}
        border-l-2 rounded-r-lg p-3 mb-2
        transition-all duration-200
      `}
    >
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          {isHistoric ? (
            <History className={`w-3 h-3 ${styles.icon}`} />
          ) : (
            <Clock className={`w-3 h-3 ${styles.icon}`} />
          )}
          <span className="text-xs text-gray-500 font-mono">
            {isHistoric ? 'Historical' : formatTime(event.timestamp)}
          </span>
        </div>
        <span className={`text-xs font-medium ${styles.text} uppercase tracking-wide`}>
          {event.event_type.replace('_', ' ')}
        </span>
      </div>
      {renderContent()}
    </div>
  );
};

// Empty state component
const EmptyState: React.FC = () => (
  <div className="flex flex-col items-center justify-center h-full text-center p-6">
    <MessageSquare className="w-12 h-12 text-gray-600 mb-3" />
    <p className="text-gray-400 text-sm">No events yet</p>
    <p className="text-gray-500 text-xs mt-1">
      Events will appear here as the agent works
    </p>
  </div>
);

// Main LiveFeed component
export const LiveFeed: React.FC<LiveFeedProps> = ({ agentId }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const shouldAutoScroll = useRef(true);

  // Get live feed events from store
  const events = useStateStore((state) => state.agents[agentId]?.liveFeed || []);

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    if (shouldAutoScroll.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [events]);

  // Handle scroll to determine if we should auto-scroll
  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    // If user scrolls up, disable auto-scroll; if at bottom, enable it
    shouldAutoScroll.current = scrollHeight - scrollTop - clientHeight < 50;
  };

  if (events.length === 0) {
    return (
      <div className="h-full bg-gray-900">
        <EmptyState />
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="h-full bg-gray-900 overflow-y-auto p-4"
      style={{ scrollBehavior: 'smooth' }}
    >
      {events.map((event) => (
        <FeedEvent key={event.id} event={event} />
      ))}
    </div>
  );
};
