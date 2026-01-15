import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, FolderGit2, Loader2, Check } from 'lucide-react';
import type { Repository } from '../../api/client';

interface RepoSelectorProps {
  repositories: Repository[];
  activeRepoId: string;
  onSelect: (repoId: string) => void;
  isLoading?: boolean;
  disabled?: boolean;
}

export const RepoSelector: React.FC<RepoSelectorProps> = ({
  repositories,
  activeRepoId,
  onSelect,
  isLoading = false,
  disabled = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const activeRepo = repositories.find((repo) => repo.id === activeRepoId);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Close on escape
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleEscape);
      return () => document.removeEventListener('keydown', handleEscape);
    }
  }, [isOpen]);

  const handleSelect = (repoId: string) => {
    if (repoId !== activeRepoId) {
      onSelect(repoId);
    }
    setIsOpen(false);
  };

  const truncatePath = (path: string, maxLength: number = 40): string => {
    if (path.length <= maxLength) return path;
    const parts = path.split('/');
    if (parts.length <= 2) return '...' + path.slice(-maxLength + 3);
    return '.../' + parts.slice(-2).join('/');
  };

  if (repositories.length === 0) {
    return (
      <div className="header-control gap-2.5 px-4 bg-gray-100 dark:bg-gray-700 rounded-lg text-gray-500 dark:text-gray-400">
        <FolderGit2 className="w-4 h-4" />
        <span className="text-sm tracking-wide">No repositories</span>
      </div>
    );
  }

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => !disabled && !isLoading && setIsOpen(!isOpen)}
        disabled={disabled || isLoading}
        className={`
          header-control gap-2.5 px-4 bg-gray-100 dark:bg-gray-700 rounded-lg
          transition-colors min-w-[200px] max-w-[350px]
          ${disabled || isLoading
            ? 'opacity-50 cursor-not-allowed'
            : 'hover:bg-gray-200 dark:hover:bg-gray-600 cursor-pointer'
          }
        `}
        title={activeRepo?.path || 'Select repository'}
      >
        {isLoading ? (
          <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
        ) : (
          <FolderGit2 className="w-4 h-4 text-gray-600 dark:text-gray-400 flex-shrink-0" />
        )}
        <div className="flex flex-col items-start min-w-0 flex-1">
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate w-full text-left">
            {activeRepo?.name || 'Select repository'}
          </span>
          {activeRepo && (
            <span
              className="text-xs text-gray-500 dark:text-gray-400 truncate w-full text-left"
              title={activeRepo.path}
            >
              {truncatePath(activeRepo.path)}
            </span>
          )}
        </div>
        <ChevronDown
          className={`w-4 h-4 text-gray-500 dark:text-gray-400 transition-transform flex-shrink-0 ${
            isOpen ? 'rotate-180' : ''
          }`}
        />
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 w-full min-w-[300px] max-w-[400px] bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 z-50 overflow-hidden">
          <div className="max-h-[300px] overflow-y-auto">
            {repositories.map((repo) => (
              <button
                key={repo.id}
                onClick={() => handleSelect(repo.id)}
                className={`
                  w-full px-4 py-3 flex items-start gap-3 text-left transition-colors
                  ${repo.id === activeRepoId
                    ? 'bg-blue-50 dark:bg-blue-900/30'
                    : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }
                `}
              >
                <FolderGit2 className={`w-4 h-4 mt-0.5 flex-shrink-0 ${
                  repo.id === activeRepoId
                    ? 'text-blue-600 dark:text-blue-400'
                    : 'text-gray-500 dark:text-gray-400'
                }`} />
                <div className="flex flex-col min-w-0 flex-1">
                  <span className={`text-sm font-medium truncate ${
                    repo.id === activeRepoId
                      ? 'text-blue-700 dark:text-blue-300'
                      : 'text-gray-900 dark:text-gray-100'
                  }`}>
                    {repo.name}
                  </span>
                  <span
                    className="text-xs text-gray-500 dark:text-gray-400 truncate"
                    title={repo.path}
                  >
                    {repo.path}
                  </span>
                </div>
                {repo.id === activeRepoId && (
                  <Check className="w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
                )}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
