import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditRuleModal } from './EditRuleModal';
import type { Rule } from '../../api/client';

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

describe('EditRuleModal', () => {
  const mockOnClose = vi.fn();
  const mockOnSave = vi.fn();

  beforeEach(() => {
    mockOnClose.mockClear();
    mockOnSave.mockClear();
    uuidCounter = 0;
  });

  describe('rendering', () => {
    it('does not render when isOpen is false', () => {
      render(
        <EditRuleModal
          isOpen={false}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      expect(screen.queryByText('Edit Rule')).not.toBeInTheDocument();
    });

    it('does not render when rule is null', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={null}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      expect(screen.queryByText('Edit Rule')).not.toBeInTheDocument();
    });

    it('renders the modal when open with a rule', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ name: 'my-rule' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      expect(screen.getByText('Edit Rule')).toBeInTheDocument();
      expect(screen.getByText('my-rule')).toBeInTheDocument();
    });

    it('displays the rule name as read-only', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ name: 'readonly-name' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      expect(screen.getByText('readonly-name')).toBeInTheDocument();
      // Name should not be editable (no input for name)
      expect(screen.queryByRole('textbox', { name: /name/i })).not.toBeInTheDocument();
    });
  });

  describe('DENY/ALLOW radio button selection', () => {
    it('displays DENY as selected when rule action is deny', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ action: 'deny' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const denyRadio = screen.getByRole('radio', { name: /deny/i });
      const allowRadio = screen.getByRole('radio', { name: /allow/i });

      expect(denyRadio).toBeChecked();
      expect(allowRadio).not.toBeChecked();
    });

    it('displays ALLOW as selected when rule action is allow', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ action: 'allow' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const denyRadio = screen.getByRole('radio', { name: /deny/i });
      const allowRadio = screen.getByRole('radio', { name: /allow/i });

      expect(denyRadio).not.toBeChecked();
      expect(allowRadio).toBeChecked();
    });

    it('switches from DENY to ALLOW when clicking ALLOW', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ action: 'deny' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const allowRadio = screen.getByRole('radio', { name: /allow/i });
      await user.click(allowRadio);

      expect(allowRadio).toBeChecked();
      expect(screen.getByRole('radio', { name: /deny/i })).not.toBeChecked();
    });

    it('switches from ALLOW to DENY when clicking DENY', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ action: 'allow' })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const denyRadio = screen.getByRole('radio', { name: /deny/i });
      await user.click(denyRadio);

      expect(denyRadio).toBeChecked();
      expect(screen.getByRole('radio', { name: /allow/i })).not.toBeChecked();
    });

    it('saves the correct action when changed', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ action: 'deny', conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Change to allow
      await user.click(screen.getByRole('radio', { name: /allow/i }));

      // Save the rule
      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          action: 'allow',
        })
      );
    });
  });

  describe('condition add/remove', () => {
    it('parses and displays existing conditions', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 2', 'type == "bug"'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Should have two condition rows
      const fieldSelects = screen.getAllByRole('combobox');
      expect(fieldSelects.length).toBeGreaterThanOrEqual(2);

      // Check the values are populated
      const valueInputs = screen.getAllByPlaceholderText('value');
      expect(valueInputs[0]).toHaveValue('2');
      expect(valueInputs[1]).toHaveValue('bug');
    });

    it('adds a new condition row when clicking Add', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Initially one condition row
      const initialValueInputs = screen.getAllByPlaceholderText('value');
      expect(initialValueInputs).toHaveLength(1);

      // Click Add button
      await user.click(screen.getByRole('button', { name: /add/i }));

      // Now should have two condition rows
      const valueInputs = screen.getAllByPlaceholderText('value');
      expect(valueInputs).toHaveLength(2);
    });

    it('removes a condition row when clicking remove (with multiple conditions)', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1', 'type == "bug"'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Initially two condition rows
      let valueInputs = screen.getAllByPlaceholderText('value');
      expect(valueInputs).toHaveLength(2);

      // Click the first remove button (there should be trash icons)
      const removeButtons = screen.getAllByTitle(/remove condition/i);
      await user.click(removeButtons[0]!);

      // Now should have one condition row
      valueInputs = screen.getAllByPlaceholderText('value');
      expect(valueInputs).toHaveLength(1);
    });

    it('disables remove button when only one condition exists', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // The remove button should be disabled
      const removeButton = screen.getByTitle(/at least one condition required/i);
      expect(removeButton).toBeDisabled();
    });

    it('allows editing field, operator, and value', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Get the selects and input
      const fieldSelects = screen.getAllByRole('combobox');
      const valueInput = screen.getByPlaceholderText('value');

      // Change field to 'type'
      await user.selectOptions(fieldSelects[0]!, 'type');
      expect(fieldSelects[0]).toHaveValue('type');

      // Change operator to '=='
      await user.selectOptions(fieldSelects[1]!, '==');
      expect(fieldSelects[1]).toHaveValue('==');

      // Change value
      await user.clear(valueInput);
      await user.type(valueInput, 'feature');
      expect(valueInput).toHaveValue('feature');
    });

    it('handles "in" operator for labels correctly', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['"frontend" in labels'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const fieldSelects = screen.getAllByRole('combobox');
      const valueInput = screen.getByPlaceholderText('value');

      // Field should be 'labels'
      expect(fieldSelects[0]).toHaveValue('labels');
      // Operator should be 'in'
      expect(fieldSelects[1]).toHaveValue('in');
      // Value should be 'frontend'
      expect(valueInput).toHaveValue('frontend');
    });
  });

  describe('validation', () => {
    it('disables save button when no valid conditions exist', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: [] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const saveButton = screen.getByRole('button', { name: /save/i });
      expect(saveButton).toBeDisabled();
    });

    it('enables save button when valid conditions exist', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const saveButton = screen.getByRole('button', { name: /save/i });
      expect(saveButton).not.toBeDisabled();
    });

    it('shows error message when trying to save with empty conditions', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Clear the value input to make condition invalid
      const valueInput = screen.getByPlaceholderText('value');
      await user.clear(valueInput);

      // Try to save - the button should be disabled now
      const saveButton = screen.getByRole('button', { name: /save/i });
      expect(saveButton).toBeDisabled();
    });

    it('filters out empty conditions when saving', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Add an empty condition
      await user.click(screen.getByRole('button', { name: /add/i }));

      // Save (the second condition is empty but first is valid)
      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          conditions: ['priority >= 1'],
        })
      );
    });
  });

  describe('save behavior', () => {
    it('calls onSave with updated rule when save is clicked', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({
            name: 'my-rule',
            conditions: ['priority >= 1'],
            action: 'deny',
            enabled: true,
          })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith({
        name: 'my-rule',
        conditions: ['priority >= 1'],
        action: 'deny',
        enabled: true,
        persisted: false, // Always false after edit
        source: 'config',
      });
    });

    it('sets persisted to false when saving', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ persisted: true, conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          persisted: false,
        })
      );
    });

    it('calls onClose after successful save', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnClose).toHaveBeenCalled();
    });

    it('saves enabled state changes', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ enabled: true, conditions: ['priority >= 1'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Toggle enabled checkbox
      const enabledCheckbox = screen.getByRole('checkbox');
      await user.click(enabledCheckbox);

      // Save
      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          enabled: false,
        })
      );
    });
  });

  describe('cancel/close behavior', () => {
    it('calls onClose when X button is clicked', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Find and click the X button
      const closeButton = screen.getByRole('button', { name: '' });
      await user.click(closeButton);

      expect(mockOnClose).toHaveBeenCalled();
    });

    it('calls onClose when Escape key is pressed', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.keyboard('{Escape}');

      expect(mockOnClose).toHaveBeenCalled();
    });

    it('calls onClose when clicking outside the modal', async () => {
      const user = userEvent.setup();
      render(
        <div data-testid="outside">
          <EditRuleModal
            isOpen={true}
            rule={createMockRule()}
            onClose={mockOnClose}
            onSave={mockOnSave}
          />
        </div>
      );

      // Click on the backdrop (the fixed overlay)
      const backdrop = document.querySelector('.fixed.inset-0');
      expect(backdrop).toBeInTheDocument();

      // Simulate click on backdrop area (outside the dialog)
      await user.click(backdrop!);

      expect(mockOnClose).toHaveBeenCalled();
    });

    it('does not save when closing', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.keyboard('{Escape}');

      expect(mockOnSave).not.toHaveBeenCalled();
    });
  });

  describe('syntax help toggle', () => {
    it('shows collapsed syntax help by default', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Syntax Help button should be visible
      expect(screen.getByText('Syntax Help')).toBeInTheDocument();

      // Content should not be expanded (check for Fields section)
      expect(screen.queryByText('Fields')).not.toBeInTheDocument();
    });

    it('expands syntax help when clicked', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Click to expand
      await user.click(screen.getByText('Syntax Help'));

      // Content should now be visible
      expect(screen.getByText('Fields')).toBeInTheDocument();
      expect(screen.getByText('Operators')).toBeInTheDocument();
    });

    it('collapses syntax help when clicked again', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule()}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Expand
      await user.click(screen.getByText('Syntax Help'));
      expect(screen.getByText('Fields')).toBeInTheDocument();

      // Collapse
      await user.click(screen.getByText('Syntax Help'));

      await waitFor(() => {
        expect(screen.queryByText('Fields')).not.toBeInTheDocument();
      });
    });

    it('inserts example when example button is clicked', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: [] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      // Expand syntax help
      await user.click(screen.getByText('Syntax Help'));

      // Click an example button
      await user.click(screen.getByRole('button', { name: /high priority/i }));

      // The condition should be inserted
      const fieldSelect = screen.getAllByRole('combobox')[0];
      expect(fieldSelect).toHaveValue('priority');
    });
  });

  describe('enabled checkbox', () => {
    it('shows enabled checkbox as checked when rule is enabled', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ enabled: true })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const checkbox = screen.getByRole('checkbox');
      expect(checkbox).toBeChecked();
    });

    it('shows enabled checkbox as unchecked when rule is disabled', () => {
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ enabled: false })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const checkbox = screen.getByRole('checkbox');
      expect(checkbox).not.toBeChecked();
    });

    it('toggles enabled state when checkbox is clicked', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ enabled: true })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      const checkbox = screen.getByRole('checkbox');
      expect(checkbox).toBeChecked();

      await user.click(checkbox);
      expect(checkbox).not.toBeChecked();

      await user.click(checkbox);
      expect(checkbox).toBeChecked();
    });
  });

  describe('condition serialization', () => {
    it('correctly formats numeric values without quotes', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['priority >= 2'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          conditions: ['priority >= 2'],
        })
      );
    });

    it('correctly formats string values with quotes', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['type == "bug"'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          conditions: ['type == "bug"'],
        })
      );
    });

    it('correctly formats "in" operator with quoted value first', async () => {
      const user = userEvent.setup();
      render(
        <EditRuleModal
          isOpen={true}
          rule={createMockRule({ conditions: ['"frontend" in labels'] })}
          onClose={mockOnClose}
          onSave={mockOnSave}
        />
      );

      await user.click(screen.getByRole('button', { name: /save/i }));

      expect(mockOnSave).toHaveBeenCalledWith(
        expect.objectContaining({
          conditions: ['"frontend" in labels'],
        })
      );
    });
  });
});
