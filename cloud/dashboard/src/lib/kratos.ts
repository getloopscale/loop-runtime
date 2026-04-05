// Kratos self-service flow helpers.
// All flows go through the public Kratos API (accounts.{domain}).
// No internal terms (Kratos, Ory) are exposed to users.

// Kratos API is served under the same API gateway domain (no CORS issues).
// MEET-2026-04-05-032 Option D: /self-service/* → ory-kratos-public via Traefik.
const ACCOUNTS_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';

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

/** Extract flow-level error messages. */
export function getFlowMessages(flow: Flow): string[] {
  return (flow.ui.messages || []).map(m => m.text);
}

/** Extract field-level error messages for a specific input. */
export function getFieldErrors(flow: Flow, fieldName: string): string[] {
  const node = flow.ui.nodes.find(n => n.attributes.name === fieldName);
  return (node?.messages || []).filter(m => m.type === 'error').map(m => m.text);
}
