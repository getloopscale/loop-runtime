// Kratos self-service flow helpers.
// All flows go through the public Kratos API (accounts.{domain}).
// No internal terms (Kratos, Ory) are exposed to users.

// Prefer dedicated ACCOUNTS_URL (Kratos may live at accounts.loopzero.dev in prod
// while API lives at api.loopzero.dev). Falls back to API_URL for same-origin dev.
import { API_URL, ACCOUNTS_URL as CONFIG_ACCOUNTS_URL } from './config';
const ACCOUNTS_URL = CONFIG_ACCOUNTS_URL || API_URL;

export interface FlowNode {
  type: string;
  group: string;
  attributes: {
    name?: string;
    type?: string;
    value?: string;
    required?: boolean;
    disabled?: boolean;
    node_type?: string;
  };
  messages: { id: number; text: string; type: string }[];
  meta: { label?: { text: string } };
}

export interface Flow {
  id: string;
  type: string;
  state?: string;
  ui: {
    action: string;
    method: string;
    nodes: FlowNode[];
    messages?: { id: number; text: string; type: string }[];
  };
}

/** Initiate a self-service flow (login, registration, recovery, verification, settings). */
export async function initFlow(flowType: string): Promise<Flow> {
  const resp = await fetch(`${ACCOUNTS_URL}/self-service/${flowType}/browser`, {
    credentials: 'include',
    redirect: 'manual',
  });
  if (resp.status === 200) return resp.json();
  // Browser flow returns 303 redirect — follow it
  if (resp.status >= 300 && resp.status < 400) {
    const location = resp.headers.get('Location');
    if (location) {
      window.location.href = location;
      return {} as Flow; // redirect will happen
    }
  }
  throw new Error(`Failed to start ${flowType} flow`);
}

/** Get an existing flow by ID. */
export async function getFlow(flowType: string, flowId: string): Promise<Flow> {
  const resp = await fetch(`${ACCOUNTS_URL}/self-service/${flowType}/flows?id=${flowId}`, {
    credentials: 'include',
  });
  if (!resp.ok) throw new Error(`Failed to get ${flowType} flow`);
  return resp.json();
}

/** Submit a flow (form action from flow.ui.action). */
export async function submitFlow(action: string, method: string, body: Record<string, string>): Promise<Flow> {
  const resp = await fetch(action, {
    method,
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body),
  });
  return resp.json();
}

/** Extract input nodes from a flow (excludes CSRF, hidden, submit). */
export function getInputNodes(flow: Flow, group?: string): FlowNode[] {
  return flow.ui.nodes.filter(n => {
    if (n.attributes.type === 'hidden') return false;
    if (n.attributes.type === 'submit') return false;
    if (n.attributes.name === 'csrf_token') return false;
    if (group && n.group !== group && n.group !== 'default') return false;
    return n.attributes.type === 'text' || n.attributes.type === 'email' || n.attributes.type === 'password';
  });
}

/** Extract CSRF token from a flow. */
export function getCsrfToken(flow: Flow): string {
  const node = flow.ui.nodes.find(n => n.attributes.name === 'csrf_token');
  return node?.attributes.value || '';
}

/** Extract flow-level error messages. Tolerates Kratos error responses
 * that don't have a `ui` field (e.g., 422 with redirect_browser_to). */
export function getFlowMessages(flow: Flow): string[] {
  return (flow?.ui?.messages || []).map(m => m.text);
}

/** Extract field-level error messages for a specific input. Null-safe. */
export function getFieldErrors(flow: Flow, fieldName: string): string[] {
  const node = flow?.ui?.nodes?.find(n => n.attributes.name === fieldName);
  return (node?.messages || []).filter(m => m.type === 'error').map(m => m.text);
}

/**
 * T0.1 EC-02 — enrich generic Kratos error messages with TCIA action context.
 *
 * Kratos sometimes returns generic phrases ("An unexpected error occurred",
 * "Internal server error") that lack the Action step required by sponsor
 * critério #6. This helper detects generic phrases and appends a context-
 * specific Action sentence. Specific Kratos messages (e.g., "Invalid email")
 * pass through unchanged — they're already actionable.
 */
const GENERIC_KRATOS_PHRASES = [
  'an unexpected error occurred',
  'internal server error',
  'something went wrong',
  'an error occurred',
];

type KratosErrorContext = 'login' | 'recovery' | 'verification' | 'registration';

const ACTION_BY_CONTEXT: Record<KratosErrorContext, string> = {
  login: 'Check your credentials and retry, or contact support@loopzero.example if it persists.',
  recovery: 'Check your email address and network connection, then retry.',
  verification: 'Request a new verification link from the sign-in page.',
  registration: 'Check your inputs and retry, or contact support if it persists.',
};

export function enrichKratosError(rawMessage: string, context: KratosErrorContext): string {
  if (!rawMessage) return rawMessage;
  const lower = rawMessage.toLowerCase();

  // EC-2 / D8 (J-04 plan 2026-06-05): Kratos `identifier_similarity_check_enabled`
  // produces messages like "the password is too similar to the user identifier".
  // Replace with product-language WITHOUT leaking "identifier similarity" /
  // "Kratos" / "credential" to UI (Rule 3).
  if (lower.includes('similar to the user identifier') || lower.includes('identifier similarity')) {
    return "Choose a password that doesn't contain your email or username.";
  }

  const isGeneric = GENERIC_KRATOS_PHRASES.some((p) => lower.includes(p));
  if (!isGeneric) return rawMessage;
  return `${rawMessage}. ${ACTION_BY_CONTEXT[context]}`;
}
