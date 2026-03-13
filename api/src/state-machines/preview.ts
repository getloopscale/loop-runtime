import { createTransitionFn, type StateMachineDefinition } from './types';

export type PreviewState =
  | 'CREATING'
  | 'ACTIVE'
  | 'EXPIRING'
  | 'DELETING'
  | 'DELETED'
  | 'FAILED'
  | 'CLEANUP_FAILED';

export type PreviewEvent =
  | 'CreationCompleted'
  | 'CreationFailed'
  | 'ExpirationTriggered'
  | 'DeletionTriggered'
  | 'CleanupCompleted'
  | 'CleanupFailed'
  | 'GarbageCollected';

const definition: StateMachineDefinition<PreviewState, PreviewEvent> = {
  transitions: new Map<PreviewState, ReadonlyMap<PreviewEvent, PreviewState>>([
    ['CREATING', new Map<PreviewEvent, PreviewState>([
      ['CreationCompleted', 'ACTIVE'],
      ['CreationFailed', 'FAILED'],
    ])],
    ['ACTIVE', new Map<PreviewEvent, PreviewState>([
      ['ExpirationTriggered', 'EXPIRING'],
      ['DeletionTriggered', 'DELETING'],
    ])],
    ['EXPIRING', new Map<PreviewEvent, PreviewState>([
      ['CleanupCompleted', 'DELETED'],
      ['CleanupFailed', 'CLEANUP_FAILED'],
    ])],
    ['FAILED', new Map<PreviewEvent, PreviewState>([
      ['DeletionTriggered', 'DELETING'],
    ])],
    ['DELETING', new Map<PreviewEvent, PreviewState>([
      ['CleanupCompleted', 'DELETED'],
      ['CleanupFailed', 'CLEANUP_FAILED'],
    ])],
    ['CLEANUP_FAILED', new Map<PreviewEvent, PreviewState>([
      ['GarbageCollected', 'DELETED'],
    ])],
  ]),
  terminalStates: new Set<PreviewState>(['DELETED']),
};

export const transitionPreview = createTransitionFn<PreviewState, PreviewEvent>('PreviewEnvironment', definition);

export const PREVIEW_TERMINAL_STATES = definition.terminalStates;
