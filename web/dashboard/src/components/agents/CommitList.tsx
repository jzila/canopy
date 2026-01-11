import React, { useState } from 'react';
import { GitCommit, ChevronDown, ChevronRight, FileText, Copy, Check } from 'lucide-react';
import type { GitCommit as GitCommitType } from '../../stores/stateStore';

interface CommitListProps {
  commits: GitCommitType[];
  showAgentId?: boolean;
  agentId?: string;
}

interface CommitItemProps {
  commit: GitCommitType;
  isExpanded: boolean;
  onToggle: () => void;
}

// Format timestamp for display
const formatTimestamp = (timestamp: string): string => {
  try {
    const date = new Date(timestamp);
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  } catch {
    return timestamp;
  }
};

// Truncate commit message for single-line display
const truncateMessage = (message: string, maxLen: number = 60): string => {
  const firstLine = message.split('\n')[0] || message;
  if (firstLine.length <= maxLen) return firstLine;
  return firstLine.slice(0, maxLen - 3) + '...';
};

// Copy to clipboard hook
const useCopyToClipboard = () => {
  const [copied, setCopied] = useState<string | null>(null);

  const copy = async (text: string, id: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(id);
      setTimeout(() => setCopied(null), 2000);
    } catch (err) {
      console.error('Failed to copy:', err);
    }
  };

  return { copied, copy };
};

// Single commit item component
const CommitItem: React.FC<CommitItemProps> = ({ commit, isExpanded, onToggle }) => {
  const { copied, copy } = useCopyToClipboard();
  const hasFiles = commit.files_changed && commit.files_changed.length > 0;

  return (
    <div className="border border-gray-700 rounded-lg mb-2 overflow-hidden">
      {/* Commit header - always visible */}
      <div
        onClick={onToggle}
        className="flex items-center gap-3 p-3 bg-gray-800/50 hover:bg-gray-800 cursor-pointer transition-colors"
      >
        {/* Expand/collapse icon */}
        <div className="flex-shrink-0 text-gray-500">
          {isExpanded ? (
            <ChevronDown className="w-4 h-4" />
          ) : (
            <ChevronRight className="w-4 h-4" />
          )}
        </div>

        {/* Commit icon */}
        <div className="flex-shrink-0 text-blue-400">
          <GitCommit className="w-4 h-4" />
        </div>

        {/* Short hash */}
        <code
          className="flex-shrink-0 text-xs font-mono text-yellow-400 bg-yellow-900/20 px-1.5 py-0.5 rounded cursor-pointer hover:bg-yellow-900/40"
          onClick={(e) => {
            e.stopPropagation();
            copy(commit.hash, commit.hash);
          }}
          title={`Click to copy full hash: ${commit.hash}`}
        >
          {copied === commit.hash ? (
            <span className="flex items-center gap-1">
              <Check className="w-3 h-3" />
              copied
            </span>
          ) : (
            commit.short_hash
          )}
        </code>

        {/* Commit message */}
        <span className="flex-1 text-sm text-gray-200 truncate" title={commit.message}>
          {truncateMessage(commit.message)}
        </span>

        {/* Files count badge */}
        {hasFiles && (
          <span className="flex-shrink-0 text-xs bg-purple-900/30 text-purple-300 px-2 py-0.5 rounded flex items-center gap-1">
            <FileText className="w-3 h-3" />
            {commit.files_changed.length}
          </span>
        )}

        {/* Timestamp */}
        <span className="flex-shrink-0 text-xs text-gray-500 font-mono">
          {formatTimestamp(commit.timestamp)}
        </span>
      </div>

      {/* Expanded details */}
      {isExpanded && (
        <div className="p-3 bg-gray-900/50 border-t border-gray-700">
          {/* Full commit message */}
          <div className="mb-3">
            <div className="text-xs text-gray-500 mb-1">Message</div>
            <pre className="text-sm text-gray-200 whitespace-pre-wrap font-sans bg-gray-800/50 rounded p-2">
              {commit.message}
            </pre>
          </div>

          {/* Author info */}
          <div className="flex items-center gap-4 mb-3 text-sm">
            <div>
              <span className="text-gray-500">Author: </span>
              <span className="text-gray-300">{commit.author}</span>
            </div>
            {commit.author_email && (
              <div>
                <span className="text-gray-500">Email: </span>
                <span className="text-gray-400">{commit.author_email}</span>
              </div>
            )}
          </div>

          {/* Full hash */}
          <div className="flex items-center gap-2 mb-3">
            <span className="text-xs text-gray-500">Hash:</span>
            <code className="text-xs font-mono text-gray-400 bg-gray-800 px-2 py-1 rounded flex items-center gap-2">
              {commit.hash}
              <button
                onClick={() => copy(commit.hash, `full-${commit.hash}`)}
                className="text-gray-500 hover:text-gray-300 transition-colors"
                title="Copy full hash"
              >
                {copied === `full-${commit.hash}` ? (
                  <Check className="w-3 h-3 text-green-400" />
                ) : (
                  <Copy className="w-3 h-3" />
                )}
              </button>
            </code>
          </div>

          {/* Files changed */}
          {hasFiles && (
            <div>
              <div className="text-xs text-gray-500 mb-2">
                Files changed ({commit.files_changed.length})
              </div>
              <div className="max-h-40 overflow-y-auto">
                {commit.files_changed.map((file, index) => (
                  <div
                    key={index}
                    className="flex items-center gap-2 text-xs py-1 px-2 hover:bg-gray-800/50 rounded"
                  >
                    <FileText className="w-3 h-3 text-purple-400 flex-shrink-0" />
                    <code className="text-gray-300 truncate" title={file}>
                      {file}
                    </code>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

// Empty state component
const EmptyState: React.FC = () => (
  <div className="flex flex-col items-center justify-center py-8 text-center">
    <GitCommit className="w-10 h-10 text-gray-600 mb-2" />
    <p className="text-gray-400 text-sm">No commits yet</p>
    <p className="text-gray-500 text-xs mt-1">
      Git commits will appear here as the agent creates them
    </p>
  </div>
);

// Main CommitList component
export const CommitList: React.FC<CommitListProps> = ({ commits }) => {
  const [expandedCommits, setExpandedCommits] = useState<Set<string>>(new Set());

  const toggleCommit = (hash: string) => {
    setExpandedCommits((prev) => {
      const next = new Set(prev);
      if (next.has(hash)) {
        next.delete(hash);
      } else {
        next.add(hash);
      }
      return next;
    });
  };

  if (!commits || commits.length === 0) {
    return <EmptyState />;
  }

  return (
    <div className="space-y-1">
      {commits.map((commit) => (
        <CommitItem
          key={commit.hash}
          commit={commit}
          isExpanded={expandedCommits.has(commit.hash)}
          onToggle={() => toggleCommit(commit.hash)}
        />
      ))}
    </div>
  );
};

// Compact commit badge for AgentCard
interface CommitBadgeProps {
  count: number;
  onClick?: () => void;
}

export const CommitBadge: React.FC<CommitBadgeProps> = ({ count, onClick }) => {
  if (count === 0) return null;

  return (
    <div
      onClick={onClick}
      className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400 cursor-pointer hover:text-blue-400 transition-colors"
      title={`${count} git commit${count !== 1 ? 's' : ''}`}
    >
      <GitCommit className="w-4 h-4" />
      <span>{count}</span>
    </div>
  );
};
