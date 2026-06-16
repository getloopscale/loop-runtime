/**
 * Shape contracts for Kratos browser-mode self-service flows.
 *
 * Extracted from pages/login.tsx (T3.1 of plan 2026-06-16-m0-j01-login-polish)
 * to keep login.tsx under the 500 LOC budget per ADR D6. The types live
 * in lib/auth/ so J-02 register + J-03 verification can reuse them.
 */

export interface FlowNode {
  group: string;
  attributes: { name?: string; type?: string; value?: string };
  messages?: { text: string; type: string }[];
  meta?: { label?: { text?: string } };
}

export interface Flow {
  id: string;
  ui: {
    action: string;
    method: string;
    nodes: FlowNode[];
    messages?: { text: string; type: string }[];
  };
}
