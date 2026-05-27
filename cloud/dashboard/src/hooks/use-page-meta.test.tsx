import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useDocumentTitle, useSetPageTitle } from './use-page-meta';

describe('useDocumentTitle (T2.4 / D9)', () => {
  it('test_sets_document_title_with_suffix', () => {
    renderHook(() => useDocumentTitle('Deployments'));
    expect(document.title).toBe('Deployments · LoopZeroCloud');
  });

  it('test_resets_to_default_when_title_is_empty', () => {
    renderHook(() => useDocumentTitle(''));
    expect(document.title).toBe('LoopZeroCloud');
  });

  it('test_legacy_useSetPageTitle_still_sets_document_title', () => {
    renderHook(() => useSetPageTitle('Audit log'));
    expect(document.title).toBe('Audit log · LoopZeroCloud');
  });
});
