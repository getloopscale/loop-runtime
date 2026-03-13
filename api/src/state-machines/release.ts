import { createTransitionFn, type StateMachineDefinition } from './types';

export type ReleaseState =
  | 'READY'
  | 'PENDING'
  | 'ACTIVE'
  | 'GIT_MIRROR_FAILED'
  | 'SYNC_FAILED'
  | 'DRIFT_ALERT'
  | 'SUPERSEDED'
  | 'ROLLING_BACK'
  | 'ROLLED_BACK'
  | 'ROLLBACK_FAILED'
  | 'CANCELLED';

export type ReleaseEvent =
  | 'GitMirrorCompleted'
  | 'GitMirrorFailed'
  | 'GitMirrorRetried'
  | 'SyncCompleted'
  | 'SyncFailed'
  | 'SyncExhausted'
  | 'NewReleaseActivated'
  | 'RollbackStarted'
  | 'RollbackCompleted'
  | 'RollbackFailed'
  | 'CancelRequested';

/** Timeouts per spec §5.2 */
export const RELEASE_TIMEOUTS = {
  /** READY for > 5 minutes => GIT_MIRROR_FAILED (GIT_PUSH_TIMEOUT) */
  GIT_PUSH_TIMEOUT_MS: 5 * 60 * 1000,
  /** PENDING for > 2 minutes => SYNC_FAILED (SYNC_TIMEOUT) */
  SYNC_TIMEOUT_MS: 2 * 60 * 1000,
  /** ROLLING_BACK for > 2 minutes => ROLLBACK_FAILED (ROLLBACK_TIMEOUT) */
  ROLLBACK_TIMEOUT_MS: 2 * 60 * 1000,
} as const;

const definition: StateMachineDefinition<ReleaseState, ReleaseEvent> = {
  transitions: new Map<ReleaseState, ReadonlyMap<ReleaseEvent, ReleaseState>>([
    ['READY', new Map<ReleaseEvent, ReleaseState>([
      ['GitMirrorCompleted', 'PENDING'],
      ['GitMirrorFailed', 'GIT_MIRROR_FAILED'],
    ])],
    ['GIT_MIRROR_FAILED', new Map<ReleaseEvent, ReleaseState>([
      ['GitMirrorRetried', 'PENDING'],
    ])],
    ['PENDING', new Map<ReleaseEvent, ReleaseState>([
      ['SyncCompleted', 'ACTIVE'],
      ['SyncFailed', 'SYNC_FAILED'],
      ['CancelRequested', 'CANCELLED'],
    ])],
    ['SYNC_FAILED', new Map<ReleaseEvent, ReleaseState>([
      ['SyncCompleted', 'PENDING'],
      ['SyncExhausted', 'DRIFT_ALERT'],
    ])],
    ['ACTIVE', new Map<ReleaseEvent, ReleaseState>([
      ['NewReleaseActivated', 'SUPERSEDED'],
      ['RollbackStarted', 'ROLLING_BACK'],
    ])],
    ['ROLLING_BACK', new Map<ReleaseEvent, ReleaseState>([
      ['RollbackCompleted', 'ROLLED_BACK'],
      ['RollbackFailed', 'ROLLBACK_FAILED'],
    ])],
  ]),
  terminalStates: new Set<ReleaseState>(['SUPERSEDED', 'ROLLED_BACK', 'CANCELLED']),
};

export const transitionRelease = createTransitionFn<ReleaseState, ReleaseEvent>('Release', definition);

export const RELEASE_TERMINAL_STATES = definition.terminalStates;
