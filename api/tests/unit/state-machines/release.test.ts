import {
  transitionRelease,
  RELEASE_TIMEOUTS,
  RELEASE_TERMINAL_STATES,
  type ReleaseState,
  type ReleaseEvent,
} from '../../../src/state-machines/release';

describe('Release state machine', () => {
  describe('valid transitions', () => {
    const validCases: Array<[ReleaseState, ReleaseEvent, ReleaseState]> = [
      ['READY', 'GitMirrorCompleted', 'PENDING'],
      ['READY', 'GitMirrorFailed', 'GIT_MIRROR_FAILED'],
      ['GIT_MIRROR_FAILED', 'GitMirrorRetried', 'PENDING'],
      ['PENDING', 'SyncCompleted', 'ACTIVE'],
      ['PENDING', 'SyncFailed', 'SYNC_FAILED'],
      ['PENDING', 'CancelRequested', 'CANCELLED'],
      ['SYNC_FAILED', 'SyncCompleted', 'PENDING'],
      ['SYNC_FAILED', 'SyncExhausted', 'DRIFT_ALERT'],
      ['ACTIVE', 'NewReleaseActivated', 'SUPERSEDED'],
      ['ACTIVE', 'RollbackStarted', 'ROLLING_BACK'],
      ['ROLLING_BACK', 'RollbackCompleted', 'ROLLED_BACK'],
      ['ROLLING_BACK', 'RollbackFailed', 'ROLLBACK_FAILED'],
    ];

    it.each(validCases)(
      '%s + %s => %s',
      (from, event, expected) => {
        expect(transitionRelease(from, event)).toBe(expected);
      },
    );
  });

  describe('invalid transitions throw', () => {
    const invalidCases: Array<[ReleaseState, ReleaseEvent]> = [
      ['READY', 'SyncCompleted'],
      ['READY', 'RollbackStarted'],
      ['PENDING', 'GitMirrorCompleted'],
      ['ACTIVE', 'SyncCompleted'],
      ['ACTIVE', 'GitMirrorFailed'],
    ];

    it.each(invalidCases)(
      '%s + %s throws',
      (from, event) => {
        expect(() => transitionRelease(from, event)).toThrow();
      },
    );
  });

  describe('terminal states reject all events', () => {
    const terminalStates: ReleaseState[] = ['SUPERSEDED', 'ROLLED_BACK', 'CANCELLED'];
    const someEvents: ReleaseEvent[] = [
      'GitMirrorCompleted', 'SyncCompleted', 'NewReleaseActivated', 'RollbackStarted',
    ];

    for (const state of terminalStates) {
      for (const event of someEvents) {
        it(`${state} + ${event} throws`, () => {
          expect(() => transitionRelease(state, event)).toThrow(/terminal/);
        });
      }
    }
  });

  describe('non-terminal states that require manual intervention', () => {
    it('DRIFT_ALERT has no outgoing transitions', () => {
      expect(() => transitionRelease('DRIFT_ALERT', 'SyncCompleted')).toThrow();
    });

    it('ROLLBACK_FAILED has no outgoing transitions', () => {
      expect(() => transitionRelease('ROLLBACK_FAILED', 'RollbackCompleted')).toThrow();
    });
  });

  describe('terminal states set', () => {
    it('contains SUPERSEDED, ROLLED_BACK, CANCELLED', () => {
      expect(RELEASE_TERMINAL_STATES.has('SUPERSEDED')).toBe(true);
      expect(RELEASE_TERMINAL_STATES.has('ROLLED_BACK')).toBe(true);
      expect(RELEASE_TERMINAL_STATES.has('CANCELLED')).toBe(true);
    });

    it('does not contain ACTIVE', () => {
      expect(RELEASE_TERMINAL_STATES.has('ACTIVE')).toBe(false);
    });
  });

  describe('timeouts', () => {
    it('GIT_PUSH_TIMEOUT is 5 minutes', () => {
      expect(RELEASE_TIMEOUTS.GIT_PUSH_TIMEOUT_MS).toBe(5 * 60 * 1000);
    });

    it('SYNC_TIMEOUT is 2 minutes', () => {
      expect(RELEASE_TIMEOUTS.SYNC_TIMEOUT_MS).toBe(2 * 60 * 1000);
    });

    it('ROLLBACK_TIMEOUT is 2 minutes', () => {
      expect(RELEASE_TIMEOUTS.ROLLBACK_TIMEOUT_MS).toBe(2 * 60 * 1000);
    });
  });
});
