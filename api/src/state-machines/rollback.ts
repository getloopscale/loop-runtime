import { createTransitionFn, type StateMachineDefinition } from './types';

export type RollbackState =
  | 'VALIDATING'
  | 'REVERTING'
  | 'SYNCING'
  | 'COMPLETED'
  | 'REJECTED'
  | 'BLOCKED'
  | 'REVERT_FAILED'
  | 'SYNC_FAILED';

export type RollbackEvent =
  | 'ValidationPassed'
  | 'UnacknowledgedEffects'
  | 'NoPreviousRelease'
  | 'RevertCompleted'
  | 'RevertFailed'
  | 'SyncCompleted'
  | 'SyncFailed';

const definition: StateMachineDefinition<RollbackState, RollbackEvent> = {
  transitions: new Map<RollbackState, ReadonlyMap<RollbackEvent, RollbackState>>([
    ['VALIDATING', new Map<RollbackEvent, RollbackState>([
      ['ValidationPassed', 'REVERTING'],
      ['UnacknowledgedEffects', 'REJECTED'],
      ['NoPreviousRelease', 'BLOCKED'],
    ])],
    ['REVERTING', new Map<RollbackEvent, RollbackState>([
      ['RevertCompleted', 'SYNCING'],
      ['RevertFailed', 'REVERT_FAILED'],
    ])],
    ['SYNCING', new Map<RollbackEvent, RollbackState>([
      ['SyncCompleted', 'COMPLETED'],
      ['SyncFailed', 'SYNC_FAILED'],
    ])],
  ]),
  terminalStates: new Set<RollbackState>(['COMPLETED', 'REJECTED', 'BLOCKED', 'REVERT_FAILED']),
};

export const transitionRollback = createTransitionFn<RollbackState, RollbackEvent>('Rollback', definition);

export const ROLLBACK_TERMINAL_STATES = definition.terminalStates;
