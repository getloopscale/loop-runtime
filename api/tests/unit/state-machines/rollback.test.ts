import {
  transitionRollback,
  ROLLBACK_TERMINAL_STATES,
  type RollbackState,
  type RollbackEvent,
} from '../../../src/state-machines/rollback';

describe('Rollback state machine', () => {
  describe('valid transitions', () => {
    const validCases: Array<[RollbackState, RollbackEvent, RollbackState]> = [
      ['VALIDATING', 'ValidationPassed', 'REVERTING'],
      ['VALIDATING', 'UnacknowledgedEffects', 'REJECTED'],
      ['VALIDATING', 'NoPreviousRelease', 'BLOCKED'],
      ['REVERTING', 'RevertCompleted', 'SYNCING'],
      ['REVERTING', 'RevertFailed', 'REVERT_FAILED'],
      ['SYNCING', 'SyncCompleted', 'COMPLETED'],
      ['SYNCING', 'SyncFailed', 'SYNC_FAILED'],
    ];

    it.each(validCases)(
      '%s + %s => %s',
      (from, event, expected) => {
        expect(transitionRollback(from, event)).toBe(expected);
      },
    );
  });

  describe('invalid transitions throw', () => {
    const invalidCases: Array<[RollbackState, RollbackEvent]> = [
      ['VALIDATING', 'RevertCompleted'],
      ['VALIDATING', 'SyncCompleted'],
      ['REVERTING', 'ValidationPassed'],
      ['REVERTING', 'SyncCompleted'],
      ['SYNCING', 'ValidationPassed'],
      ['SYNCING', 'RevertCompleted'],
    ];

    it.each(invalidCases)(
      '%s + %s throws',
      (from, event) => {
        expect(() => transitionRollback(from, event)).toThrow();
      },
    );
  });

  describe('terminal states reject all events', () => {
    const terminalStates: RollbackState[] = ['COMPLETED', 'REJECTED', 'BLOCKED', 'REVERT_FAILED'];
    const allEvents: RollbackEvent[] = [
      'ValidationPassed', 'UnacknowledgedEffects', 'NoPreviousRelease',
      'RevertCompleted', 'RevertFailed', 'SyncCompleted', 'SyncFailed',
    ];

    for (const state of terminalStates) {
      for (const event of allEvents) {
        it(`${state} + ${event} throws`, () => {
          expect(() => transitionRollback(state, event)).toThrow(/terminal/);
        });
      }
    }
  });

  describe('SYNC_FAILED is not terminal', () => {
    it('is not in terminal states set', () => {
      expect(ROLLBACK_TERMINAL_STATES.has('SYNC_FAILED' as any)).toBe(false);
    });

    it('has no outgoing transitions (requires manual intervention)', () => {
      expect(() => transitionRollback('SYNC_FAILED', 'SyncCompleted')).toThrow();
    });
  });

  describe('terminal states set', () => {
    it('contains COMPLETED, REJECTED, BLOCKED, REVERT_FAILED', () => {
      expect(ROLLBACK_TERMINAL_STATES.has('COMPLETED')).toBe(true);
      expect(ROLLBACK_TERMINAL_STATES.has('REJECTED')).toBe(true);
      expect(ROLLBACK_TERMINAL_STATES.has('BLOCKED')).toBe(true);
      expect(ROLLBACK_TERMINAL_STATES.has('REVERT_FAILED')).toBe(true);
    });

    it('has exactly 4 terminal states', () => {
      expect(ROLLBACK_TERMINAL_STATES.size).toBe(4);
    });
  });
});
