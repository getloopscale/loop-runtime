import { createTransitionFn, type StateMachineDefinition } from './types';

export type PromotionState =
  | 'VALIDATING'
  | 'COMMITTING'
  | 'SYNCING'
  | 'COMPLETED'
  | 'REJECTED'
  | 'COMMIT_FAILED'
  | 'SYNC_FAILED';

export type PromotionEvent =
  | 'ValidationPassed'
  | 'ValidationFailed'
  | 'CommitCompleted'
  | 'CommitFailed'
  | 'SyncCompleted'
  | 'SyncFailed';

const definition: StateMachineDefinition<PromotionState, PromotionEvent> = {
  transitions: new Map<PromotionState, ReadonlyMap<PromotionEvent, PromotionState>>([
    ['VALIDATING', new Map<PromotionEvent, PromotionState>([
      ['ValidationPassed', 'COMMITTING'],
      ['ValidationFailed', 'REJECTED'],
    ])],
    ['COMMITTING', new Map<PromotionEvent, PromotionState>([
      ['CommitCompleted', 'SYNCING'],
      ['CommitFailed', 'COMMIT_FAILED'],
    ])],
    ['SYNCING', new Map<PromotionEvent, PromotionState>([
      ['SyncCompleted', 'COMPLETED'],
      ['SyncFailed', 'SYNC_FAILED'],
    ])],
  ]),
  terminalStates: new Set<PromotionState>(['COMPLETED', 'REJECTED', 'COMMIT_FAILED']),
};

export const transitionPromotion = createTransitionFn<PromotionState, PromotionEvent>('Promotion', definition);

export const PROMOTION_TERMINAL_STATES = definition.terminalStates;
