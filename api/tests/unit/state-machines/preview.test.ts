import {
  transitionPreview,
  PREVIEW_TERMINAL_STATES,
  type PreviewState,
  type PreviewEvent,
} from '../../../src/state-machines/preview';

describe('PreviewEnvironment state machine', () => {
  describe('valid transitions', () => {
    const validCases: Array<[PreviewState, PreviewEvent, PreviewState]> = [
      ['CREATING', 'CreationCompleted', 'ACTIVE'],
      ['CREATING', 'CreationFailed', 'FAILED'],
      ['ACTIVE', 'ExpirationTriggered', 'EXPIRING'],
      ['ACTIVE', 'DeletionTriggered', 'DELETING'],
      ['EXPIRING', 'CleanupCompleted', 'DELETED'],
      ['EXPIRING', 'CleanupFailed', 'CLEANUP_FAILED'],
      ['FAILED', 'DeletionTriggered', 'DELETING'],
      ['DELETING', 'CleanupCompleted', 'DELETED'],
      ['DELETING', 'CleanupFailed', 'CLEANUP_FAILED'],
      ['CLEANUP_FAILED', 'GarbageCollected', 'DELETED'],
    ];

    it.each(validCases)(
      '%s + %s => %s',
      (from, event, expected) => {
        expect(transitionPreview(from, event)).toBe(expected);
      },
    );
  });

  describe('invalid transitions throw', () => {
    const invalidCases: Array<[PreviewState, PreviewEvent]> = [
      ['CREATING', 'ExpirationTriggered'],
      ['CREATING', 'CleanupCompleted'],
      ['ACTIVE', 'CreationCompleted'],
      ['ACTIVE', 'CleanupCompleted'],
      ['EXPIRING', 'DeletionTriggered'],
      ['FAILED', 'CreationCompleted'],
      ['CLEANUP_FAILED', 'CleanupCompleted'],
    ];

    it.each(invalidCases)(
      '%s + %s throws',
      (from, event) => {
        expect(() => transitionPreview(from, event)).toThrow();
      },
    );
  });

  describe('terminal state DELETED rejects all events', () => {
    const allEvents: PreviewEvent[] = [
      'CreationCompleted', 'CreationFailed', 'ExpirationTriggered',
      'DeletionTriggered', 'CleanupCompleted', 'CleanupFailed', 'GarbageCollected',
    ];

    for (const event of allEvents) {
      it(`DELETED + ${event} throws`, () => {
        expect(() => transitionPreview('DELETED', event)).toThrow(/terminal/);
      });
    }
  });

  describe('terminal states set', () => {
    it('contains only DELETED', () => {
      expect(PREVIEW_TERMINAL_STATES.has('DELETED')).toBe(true);
      expect(PREVIEW_TERMINAL_STATES.size).toBe(1);
    });
  });
});
