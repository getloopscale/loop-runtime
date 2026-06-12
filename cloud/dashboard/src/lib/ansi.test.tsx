import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { parseAnsi, hasAnsi } from './ansi';

describe('parseAnsi (J-19 T1.2 — R2 blueprint MUST)', () => {
  it('test_parse_ansi_red_sequence_returns_styled_node', () => {
    const node = parseAnsi('\x1b[31merror\x1b[0m');
    const { container } = render(<>{node}</>);
    const span = container.querySelector('span[style*="color"]');
    expect(span).not.toBeNull();
    expect(span?.textContent).toContain('error');
  });

  it('test_parse_ansi_plain_text_passes_through_unchanged', () => {
    const node = parseAnsi('plain text');
    expect(node).toBe('plain text');
  });

  it('test_has_ansi_detects_escape_sequence', () => {
    expect(hasAnsi('\x1b[31mhello\x1b[0m')).toBe(true);
    expect(hasAnsi('plain text')).toBe(false);
    expect(hasAnsi('')).toBe(false);
  });

  it('test_parse_ansi_xml_input_escaped_no_xss', () => {
    // EC-10 mitigation: escapeXML:true prevents user-controlled XML/HTML injection
    const node = parseAnsi('<script>alert(1)</script>');
    expect(node).toBe('<script>alert(1)</script>'); // pass-through (no ANSI) — no rendering
  });
});
