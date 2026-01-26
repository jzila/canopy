import React, { useState, useMemo } from 'react';
import { GitCommit, ChevronDown, ChevronRight, FileText, Copy, Check, Code } from 'lucide-react';
import type { GitCommit as GitCommitType } from '../../stores/stateStore';

// Parse a unified diff into per-file hunks
interface DiffHunk {
  header: string;
  lines: string[];
}

interface FileDiff {
  filename: string;
  hunks: DiffHunk[];
}

const parseDiff = (patch: string): FileDiff[] => {
  if (!patch || patch.trim() === '') return [];

  const files: FileDiff[] = [];
  const lines = patch.split('\n');
  let currentFile: FileDiff | null = null;
  let currentHunk: DiffHunk | null = null;

  for (const line of lines) {
    // New file header (diff --git a/... b/...)
    if (line.startsWith('diff --git')) {
      if (currentFile) {
        if (currentHunk) currentFile.hunks.push(currentHunk);
        files.push(currentFile);
      }
      // Extract filename from "diff --git a/path b/path"
      const match = line.match(/diff --git a\/(.+?) b\//);
      currentFile = {
        filename: (match && match[1]) ? match[1] : 'unknown',
        hunks: [],
      };
      currentHunk = null;
      continue;
    }

    // Hunk header (@@ -x,y +a,b @@)
    if (line.startsWith('@@')) {
      if (currentHunk && currentFile) {
        currentFile.hunks.push(currentHunk);
      }
      currentHunk = {
        header: line,
        lines: [],
      };
      continue;
    }

    // Skip file metadata lines (---, +++, index, etc.)
    if (line.startsWith('---') || line.startsWith('+++') ||
        line.startsWith('index ') || line.startsWith('new file') ||
        line.startsWith('deleted file') || line.startsWith('Binary files')) {
      continue;
    }

    // Add content lines to current hunk
    if (currentHunk) {
      currentHunk.lines.push(line);
    }
  }

  // Push final file and hunk
  if (currentFile) {
    if (currentHunk) currentFile.hunks.push(currentHunk);
    files.push(currentFile);
  }

  return files;
};

// Get line style based on diff prefix
const getDiffLineStyle = (line: string): string => {
  if (line.startsWith('+')) {
    return 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-200';
  }
  if (line.startsWith('-')) {
    return 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-200';
  }
  return 'text-gray-700 dark:text-gray-300';
};

// Component for rendering a single file's diff
interface FileDiffViewProps {
  file: FileDiff;
  defaultExpanded?: boolean;
}

const FileDiffView: React.FC<FileDiffViewProps> = ({ file, defaultExpanded = true }) => {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const lineCount = file.hunks.reduce((acc, hunk) => acc + hunk.lines.length, 0);
  const additions = file.hunks.reduce(
    (acc, hunk) => acc + hunk.lines.filter(l => l.startsWith('+')).length, 0
  );
  const deletions = file.hunks.reduce(
    (acc, hunk) => acc + hunk.lines.filter(l => l.startsWith('-')).length, 0
  );

  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded mb-2 overflow-hidden">
      {/* File header */}
      <div
        onClick={() => setIsExpanded(!isExpanded)}
        className="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-800 cursor-pointer hover:bg-gray-150 dark:hover:bg-gray-750 transition-colors"
      >
        <div className="text-gray-400 dark:text-gray-500">
          {isExpanded ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
        </div>
        <Code className="w-3 h-3 text-purple-600 dark:text-purple-400" />
        <code className="text-xs font-mono text-gray-700 dark:text-gray-300 flex-1 truncate" title={file.filename}>
          {file.filename}
        </code>
        <div className="flex items-center gap-2 text-xs">
          {additions > 0 && (
            <span className="text-green-600 dark:text-green-400">+{additions}</span>
          )}
          {deletions > 0 && (
            <span className="text-red-600 dark:text-red-400">-{deletions}</span>
          )}
          <span className="text-gray-400 dark:text-gray-500">({lineCount} lines)</span>
        </div>
      </div>

      {/* File diff content */}
      {isExpanded && (
        <div className="overflow-x-auto">
          <pre className="text-xs font-mono p-0 m-0 bg-gray-50 dark:bg-gray-900/50">
            {file.hunks.map((hunk, hunkIdx) => (
              <div key={hunkIdx}>
                {/* Hunk header */}
                <div className="px-3 py-1 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 border-y border-gray-200 dark:border-gray-700">
                  {hunk.header}
                </div>
                {/* Hunk lines */}
                {hunk.lines.map((line, lineIdx) => (
                  <div
                    key={lineIdx}
                    className={`px-3 py-0.5 whitespace-pre ${getDiffLineStyle(line)}`}
                  >
                    {line || ' '}
                  </div>
                ))}
              </div>
            ))}
          </pre>
        </div>
      )}
    </div>
  );
};

// Component for the full diff view
interface DiffViewProps {
  patch?: string | undefined;
}

const DiffView: React.FC<DiffViewProps> = ({ patch }) => {
  const [allExpanded, setAllExpanded] = useState(true);

  const fileDiffs = useMemo(() => parseDiff(patch || ''), [patch]);

  // Handle empty or no patch
  if (!patch || patch.trim() === '') {
    return (
      <div className="text-xs text-gray-400 dark:text-gray-500 italic py-2">
        No diff available for this commit
      </div>
    );
  }

  if (fileDiffs.length === 0) {
    return (
      <div className="text-xs text-gray-400 dark:text-gray-500 italic py-2">
        Unable to parse diff
      </div>
    );
  }

  const totalFiles = fileDiffs.length;
  const shouldCollapseByDefault = totalFiles > 3;

  return (
    <div>
      {/* Diff header with expand/collapse all */}
      <div className="flex items-center justify-between mb-2">
        <div className="text-xs text-gray-500">
          Diff ({totalFiles} file{totalFiles !== 1 ? 's' : ''})
        </div>
        {totalFiles > 1 && (
          <button
            onClick={() => setAllExpanded(!allExpanded)}
            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors"
          >
            {allExpanded ? 'Collapse all' : 'Expand all'}
          </button>
        )}
      </div>

      {/* Per-file diffs */}
      <div className="max-h-96 overflow-y-auto">
        {fileDiffs.map((file, idx) => (
          <FileDiffView
            key={`${file.filename}-${idx}`}
            file={file}
            defaultExpanded={allExpanded && !shouldCollapseByDefault}
          />
        ))}
      </div>
    </div>
  );
};

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
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg mb-2 overflow-hidden">
      {/* Commit header - always visible */}
      <div
        onClick={onToggle}
        className="flex items-center gap-3 p-3 bg-gray-100/50 dark:bg-gray-800/50 hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors"
      >
        {/* Expand/collapse icon */}
        <div className="flex-shrink-0 text-gray-400 dark:text-gray-500">
          {isExpanded ? (
            <ChevronDown className="w-4 h-4" />
          ) : (
            <ChevronRight className="w-4 h-4" />
          )}
        </div>

        {/* Commit icon */}
        <div className="flex-shrink-0 text-blue-600 dark:text-blue-400">
          <GitCommit className="w-4 h-4" />
        </div>

        {/* Short hash */}
        <code
          className="flex-shrink-0 text-xs font-mono text-yellow-700 dark:text-yellow-400 bg-yellow-100 dark:bg-yellow-900/20 px-1.5 py-0.5 rounded cursor-pointer hover:bg-yellow-200 dark:hover:bg-yellow-900/40"
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
        <span className="flex-1 text-sm text-gray-800 dark:text-gray-200 truncate" title={commit.message}>
          {truncateMessage(commit.message)}
        </span>

        {/* Files count badge */}
        {hasFiles && (
          <span className="flex-shrink-0 text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 px-2 py-0.5 rounded flex items-center gap-1">
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
        <div className="p-3 bg-gray-50 dark:bg-gray-900/50 border-t border-gray-200 dark:border-gray-700">
          {/* Full commit message */}
          <div className="mb-3">
            <div className="text-xs text-gray-500 mb-1">Message</div>
            <pre className="text-sm text-gray-800 dark:text-gray-200 whitespace-pre-wrap font-sans bg-gray-100 dark:bg-gray-800/50 rounded p-2">
              {commit.message}
            </pre>
          </div>

          {/* Author info */}
          <div className="flex items-center gap-4 mb-3 text-sm">
            <div>
              <span className="text-gray-500">Author: </span>
              <span className="text-gray-700 dark:text-gray-300">{commit.author}</span>
            </div>
            {commit.author_email && (
              <div>
                <span className="text-gray-500">Email: </span>
                <span className="text-gray-500 dark:text-gray-400">{commit.author_email}</span>
              </div>
            )}
          </div>

          {/* Full hash */}
          <div className="flex items-center gap-2 mb-3">
            <span className="text-xs text-gray-500">Hash:</span>
            <code className="text-xs font-mono text-gray-500 dark:text-gray-400 bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded flex items-center gap-2">
              {commit.hash}
              <button
                onClick={() => copy(commit.hash, `full-${commit.hash}`)}
                className="text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                title="Copy full hash"
              >
                {copied === `full-${commit.hash}` ? (
                  <Check className="w-3 h-3 text-green-600 dark:text-green-400" />
                ) : (
                  <Copy className="w-3 h-3" />
                )}
              </button>
            </code>
          </div>

          {/* Files changed (shown only when no patch available) */}
          {hasFiles && !commit.patch && (
            <div className="mb-3">
              <div className="text-xs text-gray-500 mb-2">
                Files changed ({commit.files_changed.length})
              </div>
              <div className="max-h-40 overflow-y-auto">
                {commit.files_changed.map((file, index) => (
                  <div
                    key={index}
                    className="flex items-center gap-2 text-xs py-1 px-2 hover:bg-gray-100 dark:hover:bg-gray-800/50 rounded"
                  >
                    <FileText className="w-3 h-3 text-purple-600 dark:text-purple-400 flex-shrink-0" />
                    <code className="text-gray-700 dark:text-gray-300 truncate" title={file}>
                      {file}
                    </code>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Diff view */}
          <DiffView patch={commit.patch} />
        </div>
      )}
    </div>
  );
};

// Empty state component
const EmptyState: React.FC = () => (
  <div className="flex flex-col items-center justify-center py-8 text-center">
    <GitCommit className="w-10 h-10 text-gray-400 dark:text-gray-600 mb-2" />
    <p className="text-gray-500 dark:text-gray-400 text-sm">No commits yet</p>
    <p className="text-gray-400 dark:text-gray-500 text-xs mt-1">
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
