import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SecretsTable } from './secrets-table';
import type { Secret } from '@/lib/api/types';

const SAMPLE: Secret[] = [
  { name: 'DATABASE_URL', environments: ['production'] } as Secret,
  { name: 'API_KEY', environments: ['production', 'staging'] } as Secret,
];

describe('SecretsTable — J-22 T3.1', () => {
  it('test_renders_one_row_per_secret', () => {
    const onRotate = vi.fn();
    const onDelete = vi.fn();
    render(<SecretsTable secrets={SAMPLE} onRotate={onRotate} onDelete={onDelete} />);
    expect(screen.getByText('DATABASE_URL')).toBeInTheDocument();
    expect(screen.getByText('API_KEY')).toBeInTheDocument();
  });

  it('test_value_column_is_masked_d1_enforced', () => {
    const onRotate = vi.fn();
    const onDelete = vi.fn();
    render(<SecretsTable secrets={SAMPLE} onRotate={onRotate} onDelete={onDelete} />);
    // D1 mandate: plaintext NEVER rendered, only masking glyph
    const masked = screen.getAllByText('••••••••');
    expect(masked.length).toBeGreaterThanOrEqual(2);
  });

  it('test_rotate_button_invokes_callback_with_secret', () => {
    const onRotate = vi.fn();
    const onDelete = vi.fn();
    render(<SecretsTable secrets={SAMPLE} onRotate={onRotate} onDelete={onDelete} />);
    fireEvent.click(screen.getByLabelText('Rotate DATABASE_URL'));
    expect(onRotate).toHaveBeenCalledTimes(1);
    expect(onRotate.mock.calls[0][0].name).toBe('DATABASE_URL');
  });

  it('test_delete_button_invokes_callback_with_secret', () => {
    const onRotate = vi.fn();
    const onDelete = vi.fn();
    render(<SecretsTable secrets={SAMPLE} onRotate={onRotate} onDelete={onDelete} />);
    fireEvent.click(screen.getByLabelText('Delete API_KEY'));
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onDelete.mock.calls[0][0].name).toBe('API_KEY');
  });
});
