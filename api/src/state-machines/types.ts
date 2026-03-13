/**
 * Generic state machine transition function type.
 * Returns the new state or throws on invalid transitions (programming errors).
 */
export type TransitionFn<TState extends string, TEvent extends string> = (
  current: TState,
  event: TEvent,
) => TState;

/**
 * A state machine definition consisting of a transition map and terminal states.
 */
export interface StateMachineDefinition<TState extends string, TEvent extends string> {
  readonly transitions: ReadonlyMap<TState, ReadonlyMap<TEvent, TState>>;
  readonly terminalStates: ReadonlySet<TState>;
}

/**
 * Creates a type-safe transition function from a state machine definition.
 * Throws on invalid transitions (programming errors per spec §5).
 */
export function createTransitionFn<TState extends string, TEvent extends string>(
  name: string,
  definition: StateMachineDefinition<TState, TEvent>,
): TransitionFn<TState, TEvent> {
  return (current: TState, event: TEvent): TState => {
    if (definition.terminalStates.has(current)) {
      throw new Error(
        `[${name}] Invalid transition: state '${current}' is terminal — cannot apply event '${event}'`,
      );
    }

    const stateTransitions = definition.transitions.get(current);
    if (!stateTransitions) {
      throw new Error(
        `[${name}] Invalid transition: no transitions defined for state '${current}'`,
      );
    }

    const nextState = stateTransitions.get(event);
    if (nextState === undefined) {
      throw new Error(
        `[${name}] Invalid transition: cannot apply event '${event}' in state '${current}'`,
      );
    }

    return nextState;
  };
}
