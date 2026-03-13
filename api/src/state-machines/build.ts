import { createTransitionFn, type StateMachineDefinition } from './types';

export type BuildState =
  | 'QUEUED'
  | 'RUNNING'
  | 'SUCCEEDED'
  | 'FAILED'
  | 'CANCELLING'
  | 'CANCELLED';

export type BuildEvent =
  | 'BuildStarted'
  | 'BuildSucceeded'
  | 'BuildFailed'
  | 'BuildCancelled'
  | 'CancelRequested'
  | 'CancelConfirmed'
  | 'CancelTimedOut';

/** Timeouts per spec §5.1 */
export const BUILD_TIMEOUTS = {
  /** QUEUED for > 5 minutes => FAILED (QUEUE_TIMEOUT) */
  QUEUE_TIMEOUT_MS: 5 * 60 * 1000,
  /** RUNNING for > 30 minutes => FAILED (BUILD_TIMEOUT) */
  BUILD_TIMEOUT_MS: 30 * 60 * 1000,
  /** CANCELLING for > 60 seconds => CANCELLED (CANCEL_TIMEOUT) */
  CANCEL_TIMEOUT_MS: 60 * 1000,
} as const;

const definition: StateMachineDefinition<BuildState, BuildEvent> = {
  transitions: new Map<BuildState, ReadonlyMap<BuildEvent, BuildState>>([
    ['QUEUED', new Map<BuildEvent, BuildState>([
      ['BuildStarted', 'RUNNING'],
      ['BuildCancelled', 'CANCELLED'],
    ])],
    ['RUNNING', new Map<BuildEvent, BuildState>([
      ['BuildSucceeded', 'SUCCEEDED'],
      ['BuildFailed', 'FAILED'],
      ['CancelRequested', 'CANCELLING'],
    ])],
    ['CANCELLING', new Map<BuildEvent, BuildState>([
      ['CancelConfirmed', 'CANCELLED'],
      ['CancelTimedOut', 'CANCELLED'],
    ])],
  ]),
  terminalStates: new Set<BuildState>(['SUCCEEDED', 'FAILED', 'CANCELLED']),
};

export const transitionBuild = createTransitionFn<BuildState, BuildEvent>('Build', definition);

export const BUILD_TERMINAL_STATES = definition.terminalStates;
