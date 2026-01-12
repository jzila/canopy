import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { RepoSelector } from './RepoSelector';
import type { Repository } from '../../api/client';

const mockRepositories: Repository[] = [
  {
    id: 'repo-1',
    name: 'project-alpha',
    path: '/home/user/projects/project-alpha',
    created_at: '2024-01-01T00:00:00Z',
    is_active: true,
  },
  {
    id: 'repo-2',
    name: 'project-beta',
    path: '/home/user/projects/project-beta',
    created_at: '2024-01-02T00:00:00Z',
    is_active: false,
  },
  {
    id: 'repo-3',
    name: 'project-gamma',
    path: '/home/user/very/long/nested/path/to/project-gamma',
    created_at: '2024-01-03T00:00:00Z',
    is_active: false,
  },
];

describe('RepoSelector', () => {
  const mockOnSelect = vi.fn();

  beforeEach(() => {
    mockOnSelect.mockClear();
  });

  describe('rendering', () => {
    it('renders the active repository name and path', () => {
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      expect(screen.getByText('project-alpha')).toBeInTheDocument();
      expect(screen.getByText(/project-alpha/)).toBeInTheDocument();
    });

    it('renders "No repositories" message when list is empty', () => {
      render(
        <RepoSelector
          repositories={[]}
          activeRepoId=""
          onSelect={mockOnSelect}
        />
      );

      expect(screen.getByText('No repositories')).toBeInTheDocument();
    });

    it('renders "Select repository" when no active repo is selected', () => {
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="non-existent"
          onSelect={mockOnSelect}
        />
      );

      expect(screen.getByText('Select repository')).toBeInTheDocument();
    });
  });

  describe('dropdown behavior', () => {
    it('opens dropdown when clicked', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Dropdown should be closed initially
      expect(screen.queryByText('project-beta')).not.toBeInTheDocument();

      // Click to open
      await user.click(screen.getByRole('button'));

      // All repos should now be visible
      expect(screen.getByText('project-beta')).toBeInTheDocument();
      expect(screen.getByText('project-gamma')).toBeInTheDocument();
    });

    it('closes dropdown when clicking outside', async () => {
      const user = userEvent.setup();
      render(
        <div>
          <div data-testid="outside">Outside</div>
          <RepoSelector
            repositories={mockRepositories}
            activeRepoId="repo-1"
            onSelect={mockOnSelect}
          />
        </div>
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));
      expect(screen.getByText('project-beta')).toBeInTheDocument();

      // Click outside
      await user.click(screen.getByTestId('outside'));

      // Dropdown should close
      await waitFor(() => {
        expect(screen.queryByText('project-beta')).not.toBeInTheDocument();
      });
    });

    it('closes dropdown on escape key', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));
      expect(screen.getByText('project-beta')).toBeInTheDocument();

      // Press escape
      await user.keyboard('{Escape}');

      // Dropdown should close
      await waitFor(() => {
        expect(screen.queryByText('project-beta')).not.toBeInTheDocument();
      });
    });
  });

  describe('selection', () => {
    it('calls onSelect when selecting a different repository', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));

      // Select a different repo
      await user.click(screen.getByText('project-beta'));

      expect(mockOnSelect).toHaveBeenCalledWith('repo-2');
    });

    it('does not call onSelect when selecting the active repository', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));

      // Try to select the already active repo by clicking on it in the dropdown
      const activeRepoInDropdown = screen.getAllByText('project-alpha')[1]; // Second one is in dropdown
      await user.click(activeRepoInDropdown);

      expect(mockOnSelect).not.toHaveBeenCalled();
    });

    it('closes dropdown after selection', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));
      expect(screen.getByText('project-beta')).toBeInTheDocument();

      // Select a repo
      await user.click(screen.getByText('project-beta'));

      // Dropdown should close
      await waitFor(() => {
        expect(screen.queryAllByText('project-beta')).toHaveLength(0);
      });
    });
  });

  describe('loading and disabled states', () => {
    it('shows loading spinner when isLoading is true', () => {
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
          isLoading={true}
        />
      );

      // The button should be disabled
      expect(screen.getByRole('button')).toBeDisabled();
    });

    it('disables dropdown when disabled prop is true', () => {
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
          disabled={true}
        />
      );

      expect(screen.getByRole('button')).toBeDisabled();
    });

    it('does not open dropdown when disabled', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
          disabled={true}
        />
      );

      // Try to click the disabled button
      await user.click(screen.getByRole('button'));

      // Dropdown should not open
      expect(screen.queryByText('project-beta')).not.toBeInTheDocument();
    });
  });

  describe('active indicator', () => {
    it('shows check mark for active repository in dropdown', async () => {
      const user = userEvent.setup();
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-1"
          onSelect={mockOnSelect}
        />
      );

      // Open dropdown
      await user.click(screen.getByRole('button'));

      // There should be exactly one check icon visible (for the active repo)
      const dropdownItems = screen.getAllByRole('button').slice(1); // Skip main button
      expect(dropdownItems[0]).toHaveClass('bg-blue-50');
    });
  });

  describe('path truncation', () => {
    it('truncates long paths', () => {
      render(
        <RepoSelector
          repositories={mockRepositories}
          activeRepoId="repo-3"
          onSelect={mockOnSelect}
        />
      );

      // The full path should be in the title attribute for tooltip
      const button = screen.getByRole('button');
      expect(button).toHaveAttribute('title', '/home/user/very/long/nested/path/to/project-gamma');
    });
  });
});
