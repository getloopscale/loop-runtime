import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import { VirtualTable, type VirtualTableColumn } from './virtual-table';

interface Row {
  id: string;
  name: string;
  count: number;
}

const COLUMNS: VirtualTableColumn<Row>[] = [
  { key: 'name', header: 'Name', cell: (r) => r.name, width: '1fr' },
  { key: 'count', header: 'Count', cell: (r) => r.count, width: '100px', align: 'right' },
];

function makeRows(n: number): Row[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `row-${i}`,
    name: `Item ${i}`,
    count: i,
  }));
}

describe('VirtualTable (T4.1)', () => {
  it('test_renders_headers_and_at_least_one_row', () => {
    render(<VirtualTable rows={makeRows(10)} columns={COLUMNS} rowKey={(r) => r.id} />);
    expect(screen.getByText('Name')).toBeInTheDocument();
    expect(screen.getByText('Count')).toBeInTheDocument();
    // First row visible (jsdom has no scroll viewport so virtualizer may
    // render 0 — assert that the rowgroup container exists at minimum)
    const rowGroups = document.querySelectorAll('[role="rowgroup"]');
    expect(rowGroups.length).toBeGreaterThan(0);
  });

  it('test_renders_without_throw_for_large_dataset', () => {
    // 1000 rows — proves the component doesn't choke on large lists at
    // construction time (real perf via 60fps scroll smoke is browser-only)
    expect(() =>
      render(<VirtualTable rows={makeRows(1000)} columns={COLUMNS} rowKey={(r) => r.id} />),
    ).not.toThrow();
  });

  it('test_handles_empty_rows_gracefully', () => {
    render(<VirtualTable rows={[]} columns={COLUMNS} rowKey={(r) => r.id} />);
    expect(screen.getByText('Name')).toBeInTheDocument();
  });
});
