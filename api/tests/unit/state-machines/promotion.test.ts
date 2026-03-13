import {
  transitionPromotion,
  PROMOTION_TERMINAL_STATES,
  type PromotionState,
  type PromotionEvent,
} from '../../../src/state-machines/promotion';

describe('Promotion state machine', () => {
  describe('valid transitions', () => {
    const validCases: Array<[PromotionState, PromotionEvent, PromotionState]> = [
      ['VALIDATING', 'ValidationPassed', 'COMMITTING'],
      ['VALIDATING', 'ValidationFailed', 'REJECTED'],
      ['COMMITTING', 'CommitCompleted', 'SYNCING'],
      ['COMMITTING', 'CommitFailed', 'COMMIT_FAILED'],
      ['SYNCING', 'SyncCompleted', 'COMPLETED'],
      ['SYNCING', 'SyncFailed', 'SYNC_FAILED'],
    ];

    it.each(validCases)(
      '%s + %s => %s',
      (from, event, expected) => {
        expect(transitionPromotion(from, event)).toBe(expected);
      },
    );
  });

  describe('invalid transitions throw', () => {
    const invalidCases: Array<[PromotionState, PromotionEvent]> = [
      ['VALIDATING', 'CommitCompleted'],
      ['VALIDATING', 'SyncCompleted'],
      ['COMMITTING', 'ValidationPassed'],
      ['COMMITTING', 'SyncCompleted'],
      ['SYNCING', 'ValidationPassed'],
      ['SYNCING', 'CommitCompleted'],
    ];

    it.each(invalidCases)(
      '%s + %s throws',
      (from, event) => {
        expect(() => transitionPromotion(from, event)).toThrow();
      },
    );
  });

  describe('terminal states reject all events', () => {
    const terminalStates: PromotionState[] = ['COMPLETED', 'REJECTED', 'COMMIT_FAILED'];
    const allEvents: PromotionEvent[] = [
      'ValidationPassed', 'ValidationFailed', 'CommitCompleted',
      'CommitFailed', 'SyncCompleted', 'SyncFailed',
    ];

    for (const state of terminalStates) {
      for (const event of allEvents) {
        it(`${state} + ${event} throws`, () => {
          expect(() => transitionPromotion(state, event)).toThrow(/terminal/);
        });
      }
    }
  });

  describe('SYNC_FAILED is not terminal', () => {
    it('is not in terminal states set', () => {
      expect(PROMOTION_TERMINAL_STATES.has('SYNC_FAILED' as any)).toBe(false);
    });

    it('has no outgoing transitions (requires manual intervention)', () => {
      expect(() => transitionPromotion('SYNC_FAILED', 'SyncCompleted')).toThrow();
    });
  });

  describe('terminal states set', () => {
    it('contains COMPLETED, REJECTED, COMMIT_FAILED', () => {
      expect(PROMOTION_TERMINAL_STATES.has('COMPLETED')).toBe(true);
      expect(PROMOTION_TERMINAL_STATES.has('REJECTED')).toBe(true);
      expect(PROMOTION_TERMINAL_STATES.has('COMMIT_FAILED')).toBe(true);
    });

    it('has exactly 3 terminal states', () => {
      expect(PROMOTION_TERMINAL_STATES.size).toBe(3);
    });
  });
});
