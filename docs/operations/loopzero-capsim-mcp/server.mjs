// loopzero-capsim-mcp — MCP server exposing the LoopZero bottleneck simulator as tools.
// The rubric/simulation live in the pure engine (../loopzero-capsim.mjs, unit-tested); this server
// is thin plumbing over the official MCP SDK. The `capsim_review` tool returns a GROUNDED
// scorecard — the LLM client narrates ON these numbers and must not invent metrics.
//   Register:  claude mcp add loopzero-capsim -- node <abs-path>/server.mjs
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { z } from 'zod';
import { scn, simulate, chaos, review, cost, comparePostures } from '../loopzero-capsim.mjs';

const scenarioShape = {
  tenants: z.number().optional().describe('total tenants (default 50000)'),
  activeFraction: z.number().optional().describe('fraction of tenants with a running pod (0-1)'),
  platformRpsPerTenant: z.number().optional().describe('control-plane RPS per tenant'),
  agentRpsPerActiveTenant: z.number().optional().describe('agent RPS per active tenant (memory/promptly)'),
  prefixDelegation: z.boolean().optional().describe('AWS VPC-CNI prefix delegation (raises pod density)'),
  maxPerCell: z.number().optional().describe('tenants per EKS cluster/cell (LoopZero default 500)'),
  cellSoftCeiling: z.number().optional().describe('operational ceiling on # of EKS clusters'),
  zones: z.number().optional().describe('availability zones'),
  regions: z.number().int().min(1).optional().describe('AWS regions spanned (1 = single-region; ≥2 = multi-region)'),
  crossRegionReplication: z.boolean().optional().describe('active-active replication (vs sharded sticky cells)'),
  bffReplicas: z.number().int().min(1).optional().describe('BFF request-tier replica count (default 6)'),
  engineReplicas: z.number().int().min(1).optional().describe('Engine API request-tier replica count (default 8)'),
};

// Infinity (a fully-saturated tier under region chaos) serializes as the string "∞", not null.
const asText = (obj) => ({ content: [{ type: 'text', text: JSON.stringify(obj, (_k, v) => (v === Infinity ? '∞' : v), 2) }] });

const server = new McpServer({ name: 'loopzero-capsim', version: '2.0.0' });

server.tool(
  'capsim_simulate',
  'Simulate LoopZero\'s AWS-first topology at a given load. Returns ranked bottlenecks (utilization ρ), pods/node + binding ceiling, cell/cluster count, app nodes, and SLA.',
  scenarioShape,
  async (args) => asText(simulate(scn(args))),
);

server.tool(
  'capsim_chaos',
  'Inject a failure: drop one AZ (mode="az"), one cell/EKS cluster (mode="cell"), or the whole region (mode="region"). Returns capacity loss, re-ranked bottlenecks, and the degraded SLA. mode="cell" shows the cell-isolation blast radius (survivors untouched).',
  { ...scenarioShape, mode: z.enum(['az', 'cell', 'region']).optional().describe('az | cell | region (default az)') },
  async ({ mode, ...args }) => asText(chaos(scn(args), mode === 'region' ? { killRegion: true } : mode === 'cell' ? { killCell: true } : { killAZ: true })),
);

server.tool(
  'capsim_review',
  'AI Design Review of LoopZero\'s architecture choice: a rubric score (0-100) + verdict (SOUND/NEEDS_REVISION/AT_RISK/CRITICAL) + grounded findings with recommendations. Narrate ON the returned ρ/SLA numbers — do NOT invent metrics.',
  scenarioShape,
  async (args) => asText(review(scn(args))),
);

server.tool(
  'capsim_cost',
  'Monthly AWS cost ($) of a scenario — Vantage RI unit prices × the engine\'s authoritative capacity (cells = ceil(tenants/maxPerCell), not a manual input). Returns total + compute/controlPlane/global buckets + node/cell counts.',
  scenarioShape,
  async (args) => asText(cost(scn(args))),
);

server.tool(
  'capsim_compare',
  'Region-posture decision table: single vs sharded vs active-active — monthly cost, region blast radius, SLA, review score, and the cost of resilience vs single-region. Answers "what does multi-region buy, at what cost?".',
  { ...scenarioShape, compareRegions: z.number().int().min(2).optional().describe('regions for the multi-region rows (default 3)') },
  async ({ compareRegions, ...args }) => asText(comparePostures(scn(args), { regions: compareRegions })),
);

await server.connect(new StdioServerTransport());
