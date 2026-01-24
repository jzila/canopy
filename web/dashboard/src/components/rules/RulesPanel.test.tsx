import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { RulesPanel } from './RulesPanel';
import type { Rule } from '../../api/client';
import * as client from '../../api/client';
import { useStateStore } from '../../stores/stateStore';

// Mock the API client
vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return {
    ...actual,
    getRules: vi.fn(),
    addRule: vi.fn(),
    updateRule: vi.fn(),
    deleteRule: vi.fn(),
    reorderRule: vi.fn(),
    saveRules: vi.fn(),
  };
});

// Mock crypto.randomUUID for consistent test IDs
let uuidCounter = 0;
vi.stubGlobal('crypto', {
  randomUUID: () => `test-uuid-${uuidCounter++}`,
});

const createMockRule = (overrides: Partial<Rule> = {}): Rule => ({
  name: 'test-rule',
  conditions: ['priority >= 2'],
  action: 'deny',
  enabled: true,
  persisted: true,
  source: 'config',
  ...overrides,
});

// Default props for RulesPanel
const defaultProps = {
  isExpanded: true,
  onToggle: vi.fn(),
  width: 300,
  isResizing: false,
  onResizeStart: vi.fn(),
};

describe('RulesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    uuidCounter = 0;

    // Reset Zustand store to initial state
    useStateStore.setState({
      rules: [],
      rulesPersistedState: true,
      isRulesLoading: false,
      showAddRuleDialog: false,
      // Set up active repository for rules to load
      activeRepoId: 'test-repo-id',
      repositories: [{ id: 'test-repo-id', name: 'Test Repo', path: '/test/repo', created_at: '2024-01-01T00:00:00Z', is_active: true }],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('collapsed view', () => {
    it('shows filter icon when collapsed', () => {
      render(<RulesPanel {...defaultProps} isExpanded={false} />);

      // Should show a button with filter icon
      const expandButton = screen.getByRole('button', { name: /expand/i });
      expect(expandButton).toBeInTheDocument();
    });

    it('shows active rule count when collapsed', () => {
      useStateStore.setState({
        rules: [
          createMockRule({ name: 'rule-1', enabled: true }),
          createMockRule({ name: 'rule-2', enabled: true }),
          createMockRule({ name: 'rule-3', enabled: false }),
        ],
        rulesPersistedState: true,
      });

      render(<RulesPanel {...defaultProps} isExpanded={false} />);

      // Should show "2" for the two enabled rules
      expect(screen.getByText('2')).toBeInTheDocument();
    });

    it('does not show active rule count when no rules are enabled', () => {
      useStateStore.setState({
        rules: [createMockRule({ name: 'rule-1', enabled: false })],
        rulesPersistedState: true,
      });

      render(<RulesPanel {...defaultProps} isExpanded={false} />);

      // Count badge should not be visible
      expect(screen.queryByText('0')).not.toBeInTheDocument();
    });

    it('calls onToggle when expand button is clicked', async () => {
      const mockOnToggle = vi.fn();
      const user = userEvent.setup();

      render(<RulesPanel {...defaultProps} isExpanded={false} onToggle={mockOnToggle} />);

      await user.click(screen.getByRole('button', { name: /expand/i }));

      expect(mockOnToggle).toHaveBeenCalled();
    });
  });

  describe('expanded view - loading state', () => {
    it('shows loading indicator while loading rules', async () => {
      // Mock API to return rules after delay
      vi.mocked(client.getRules).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve({ rules: [], persisted: true }), 100))
      );

      // Set loading state manually
      useStateStore.setState({ isRulesLoading: true });

      render(<RulesPanel {...defaultProps} />);

      expect(screen.getByText(/loading rules/i)).toBeInTheDocument();
    });
  });

  describe('expanded view - rules display', () => {
    it('loads rules when expanded', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1' }),
        createMockRule({ name: 'rule-2' }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      await waitFor(() => {
        expect(client.getRules).toHaveBeenCalled();
      });
    });

    it('displays rules when loaded', async () => {
      const mockRules = [
        createMockRule({ name: 'high-priority-rule', conditions: ['priority <= 1'] }),
        createMockRule({ name: 'bug-filter', conditions: ['type == "bug"'] }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to be loaded and displayed
      await waitFor(() => {
        expect(screen.getByText('high-priority-rule')).toBeInTheDocument();
        expect(screen.getByText('bug-filter')).toBeInTheDocument();
      });
    });

    it('shows empty state when no rules exist', async () => {
      vi.mocked(client.getRules).mockResolvedValue({ rules: [], persisted: true });

      render(<RulesPanel {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/no rules defined/i)).toBeInTheDocument();
      });
    });

    it('shows add rule link in empty state', async () => {
      vi.mocked(client.getRules).mockResolvedValue({ rules: [], persisted: true });

      render(<RulesPanel {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/add a rule/i)).toBeInTheDocument();
      });
    });

    it('displays error when loading fails', async () => {
      vi.mocked(client.getRules).mockRejectedValue(new Error('Network error'));

      render(<RulesPanel {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText(/network error/i)).toBeInTheDocument();
      });
    });
  });

  describe('persisted indicator display', () => {
    it('shows yellow dot indicator for unpersisted rule in RuleItem', async () => {
      const mockRules = [
        createMockRule({ name: 'unpersisted-rule', persisted: false }),
        createMockRule({ name: 'persisted-rule', persisted: true }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: false });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to be loaded
      await waitFor(() => {
        expect(screen.getByText('unpersisted-rule')).toBeInTheDocument();
      });

      // The unpersisted rule should have a yellow dot indicator
      // Check for the title attribute on the yellow dot
      const yellowDots = screen.getAllByTitle(/unsaved changes/i);
      expect(yellowDots).toHaveLength(1);
    });

    it('does not show yellow dot for persisted rules', async () => {
      const mockRules = [
        createMockRule({ name: 'persisted-rule', persisted: true }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to be loaded
      await waitFor(() => {
        expect(screen.getByText('persisted-rule')).toBeInTheDocument();
      });

      // Should not have any yellow dot indicators
      expect(screen.queryByTitle(/unsaved changes/i)).not.toBeInTheDocument();
    });
  });

  describe('Save button visibility based on list-level persisted state', () => {
    it('shows Save button when rulesPersistedState is false', async () => {
      const mockRules = [createMockRule({ name: 'rule-1', persisted: true })];

      // Mock the API to return with persisted=false
      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: false });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Save button should be visible because persisted=false
      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument();
    });

    it('shows Save button when any rule has persisted=false', async () => {
      const mockRules = [
        createMockRule({ name: 'persisted-rule', persisted: true }),
        createMockRule({ name: 'unpersisted-rule', persisted: false }),
      ];

      // Note: API returns persisted=true but one rule has persisted=false
      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('persisted-rule')).toBeInTheDocument();
      });

      // Should still show Save button because one rule is unpersisted
      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument();
    });

    it('hides Save button when all rules are persisted and list state is persisted', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1', persisted: true }),
        createMockRule({ name: 'rule-2', persisted: true }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Save button should not be visible
      expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument();
    });

    it('calls saveRules API when Save button is clicked', async () => {
      const user = userEvent.setup();
      const mockRules = [createMockRule({ name: 'rule-1', persisted: false })];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: false });
      vi.mocked(client.saveRules).mockResolvedValue({ success: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      const saveButton = screen.getByRole('button', { name: /save/i });
      await user.click(saveButton);

      // The store has the current rules, and saveRules should be called with them
      await waitFor(() => {
        expect(client.saveRules).toHaveBeenCalled();
      });
    });

    it('reloads rules after successful save', async () => {
      const user = userEvent.setup();
      const mockRules = [createMockRule({ name: 'rule-1', persisted: false })];
      const persistedRules = [createMockRule({ name: 'rule-1', persisted: true })];

      vi.mocked(client.getRules)
        .mockResolvedValueOnce({ rules: mockRules, persisted: false })
        .mockResolvedValueOnce({ rules: persistedRules, persisted: true });
      vi.mocked(client.saveRules).mockResolvedValue({ success: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      const saveButton = screen.getByRole('button', { name: /save/i });
      await user.click(saveButton);

      await waitFor(() => {
        // getRules should be called again after save
        expect(client.getRules).toHaveBeenCalledTimes(2);
      });
    });
  });

  describe('drag-and-drop reordering', () => {
    it('marks rules as unpersisted after reordering', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1', persisted: true }),
        createMockRule({ name: 'rule-2', persisted: true }),
      ];

      useStateStore.setState({
        rules: mockRules,
        rulesPersistedState: true,
      });

      // Simulate a reorder through the store action
      const { reorderRules } = useStateStore.getState();
      reorderRules(0, 1);

      // After reorder, rulesPersistedState should be false
      const state = useStateStore.getState();
      expect(state.rulesPersistedState).toBe(false);
    });

    it('displays drag handles on each rule', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1' }),
        createMockRule({ name: 'rule-2' }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load first
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Check for drag handles (by title)
      const dragHandles = screen.getAllByTitle(/drag to reorder/i);
      expect(dragHandles).toHaveLength(2);
    });

    it('updates store state after simulated reorder', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1' }),
        createMockRule({ name: 'rule-2' }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Simulate the handleDragEnd behavior directly through store
      // In real tests, we would trigger the actual DnD event
      const { reorderRules } = useStateStore.getState();
      reorderRules(0, 1);

      // Check that the store was updated
      const state = useStateStore.getState();
      expect(state.rules[0]!.name).toBe('rule-2');
      expect(state.rules[1]!.name).toBe('rule-1');
    });

    it('loads rules when component is expanded', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1' }),
        createMockRule({ name: 'rule-2' }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // The component will call getRules on mount when expanded
      await waitFor(() => {
        expect(client.getRules).toHaveBeenCalled();
      });
    });
  });

  describe('rule actions', () => {
    it('opens edit modal when edit button is clicked', async () => {
      const user = userEvent.setup();
      const mockRules = [createMockRule({ name: 'editable-rule' })];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('editable-rule')).toBeInTheDocument();
      });

      // Find and click edit button
      const editButton = screen.getByTitle(/edit rule/i);
      await user.click(editButton);

      // Edit modal should appear
      await waitFor(() => {
        expect(screen.getByText('Edit Rule')).toBeInTheDocument();
      });
    });

    it('shows delete button only for unpersisted rules', async () => {
      const mockRules = [
        createMockRule({ name: 'persisted-rule', persisted: true }),
        createMockRule({ name: 'unpersisted-rule', persisted: false }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: false });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('persisted-rule')).toBeInTheDocument();
      });

      // Should only have one delete button (for unpersisted rule)
      const deleteButtons = screen.getAllByTitle(/delete rule/i);
      expect(deleteButtons).toHaveLength(1);
    });

    it('toggles rule enabled state when power button is clicked', async () => {
      const user = userEvent.setup();
      const mockRules = [createMockRule({ name: 'toggle-rule', enabled: true })];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });
      vi.mocked(client.updateRule).mockResolvedValue({ success: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('toggle-rule')).toBeInTheDocument();
      });

      // Find and click toggle button
      const toggleButton = screen.getByTitle(/disable rule/i);
      await user.click(toggleButton);

      await waitFor(() => {
        expect(client.updateRule).toHaveBeenCalledWith('/test/repo', 'toggle-rule', { enabled: false });
      });
    });
  });

  describe('add rule dialog', () => {
    it('opens add dialog when add button is clicked', async () => {
      const user = userEvent.setup();

      vi.mocked(client.getRules).mockResolvedValue({ rules: [], persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for component to be ready
      await waitFor(() => {
        expect(screen.getByTitle(/add rule/i)).toBeInTheDocument();
      });

      // Find and click add button
      const addButton = screen.getByTitle(/add rule/i);
      await user.click(addButton);

      // The store should have showAddRuleDialog set to true
      const state = useStateStore.getState();
      expect(state.showAddRuleDialog).toBe(true);
    });

    it('opens add dialog from empty state link', async () => {
      const user = userEvent.setup();

      vi.mocked(client.getRules).mockResolvedValue({ rules: [], persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for empty state to appear
      await waitFor(() => {
        expect(screen.getByText(/no rules defined/i)).toBeInTheDocument();
      });

      // Find and click the "Add a rule" link in empty state
      const addLink = screen.getByText(/add a rule/i);
      await user.click(addLink);

      const state = useStateStore.getState();
      expect(state.showAddRuleDialog).toBe(true);
    });
  });

  describe('refresh functionality', () => {
    it('reloads rules when refresh button is clicked', async () => {
      const user = userEvent.setup();
      const mockRules = [createMockRule({ name: 'rule-1' })];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });
      expect(client.getRules).toHaveBeenCalledTimes(1);

      // Click refresh
      const refreshButton = screen.getByTitle(/refresh rules/i);
      await user.click(refreshButton);

      await waitFor(() => {
        expect(client.getRules).toHaveBeenCalledTimes(2);
      });
    });
  });

  describe('rule count display', () => {
    it('shows total rule count in header', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1' }),
        createMockRule({ name: 'rule-2' }),
        createMockRule({ name: 'rule-3' }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load and header to update
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Header should show rule count
      expect(screen.getByText('Rules (3)')).toBeInTheDocument();
    });

    it('shows active rule count badge', async () => {
      const mockRules = [
        createMockRule({ name: 'rule-1', enabled: true }),
        createMockRule({ name: 'rule-2', enabled: false }),
        createMockRule({ name: 'rule-3', enabled: true }),
      ];

      vi.mocked(client.getRules).mockResolvedValue({ rules: mockRules, persisted: true });

      render(<RulesPanel {...defaultProps} />);

      // Wait for rules to load
      await waitFor(() => {
        expect(screen.getByText('rule-1')).toBeInTheDocument();
      });

      // Should show "2" for active rules (in the header badge)
      const badges = screen.getAllByText('2');
      expect(badges.length).toBeGreaterThan(0);
    });
  });
});
