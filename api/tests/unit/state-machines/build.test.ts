import {
  transitionBuild,
  BUILD_TIMEOUTS,
  BUILD_TERMINAL_STATES,
  type BuildState,
  type BuildEvent,
} from '../../../src/state-machines/build';

describe('Build state machine', () => {
  describe('valid transitions', () => {
    const validCases: Array<[BuildState, BuildEvent, BuildState]> = [
      ['QUEUED', 'BuildStarted', 'RUNNING'],
      ['QUEUED', 'BuildCancelled', 'CANCELLED'],
      ['RUNNING', 'BuildSucceeded', 'SUCCEEDED'],
      ['RUNNING', 'BuildFailed', 'FAILED'],
      ['RUNNING', 'CancelRequested', 'CANCELLING'],
      ['CANCELLING', 'CancelConfirmed', 'CANCELLED'],
      ['CANCELLING', 'CancelTimedOut', 'CANCELLED'],
    ];

    it.each(validCases)(
      '%s + %s => %s',
      (from, event, expected) => {
        expect(transitionBuild(from, event)).toBe(expected);
      },
    );
  });

  describe('invalid transitions throw', () => {
    const invalidCases: Array<[BuildState, BuildEvent]> = [
      ['QUEUED', 'BuildSucceeded'],
      ['QUEUED', 'BuildFailed'],
      ['RUNNING', 'BuildStarted'],
      ['RUNNING', 'BuildCancelled'],
      ['CANCELLING', 'BuildStarted'],
      ['CANCELLING', 'BuildSucceeded'],
    ];

    it.each(invalidCases)(
      '%s + %s throws',
      (from, event) => {
        expect(() => transitionBuild(from, event)).toThrow();
      },
    );
  });

  describe('terminal states reject all events', () => {
    const terminalStates: BuildState[] = ['SUCCEEDED', 'FAILED', 'CANCELLED'];
    const allEvents: BuildEvent[] = [
      'BuildStarted', 'BuildSucceeded', 'BuildFailed',
      'BuildCancelled', 'CancelRequested', 'CancelConfirmed', 'CancelTimedOut',
    ];

    for (const state of terminalStates) {
      for (const event of allEvents) {
        it(`${state} + ${event} throws`, () => {
          expect(() => transitionBuild(state, event)).toThrow(/terminal/);
        });
      }
    }
  });

  describe('terminal states set', () => {
    it('contains SUCCEEDED, FAILED, CANCELLED', () => {
      expect(BUILD_TERMINAL_STATES.has('SUCCEEDED')).toBe(true);
      expect(BUILD_TERMINAL_STATES.has('FAILED')).toBe(true);
      expect(BUILD_TERMINAL_STATES.has('CANCELLED')).toBe(true);
    });

    it('does not contain non-terminal states', () => {
      expect(BUILD_TERMINAL_STATES.has('QUEUED')).toBe(false);
      expect(BUILD_TERMINAL_STATES.has('RUNNING')).toBe(false);
      expect(BUILD_TERMINAL_STATES.has('CANCELLING')).toBe(false);
    });
  });

  describe('timeouts', () => {
    it('QUEUE_TIMEOUT is 5 minutes', () => {
      expect(BUILD_TIMEOUTS.QUEUE_TIMEOUT_MS).toBe(5 * 60 * 1000);
    });

    it('BUILD_TIMEOUT is 30 minutes', () => {
      expect(BUILD_TIMEOUTS.BUILD_TIMEOUT_MS).toBe(30 * 60 * 1000);
    });

    it('CANCEL_TIMEOUT is 60 seconds', () => {
      expect(BUILD_TIMEOUTS.CANCEL_TIMEOUT_MS).toBe(60 * 1000);
    });
  });
});
