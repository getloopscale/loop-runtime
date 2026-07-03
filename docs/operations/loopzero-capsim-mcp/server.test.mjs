// Integration test: spawn the MCP server over stdio (real JSON-RPC handshake) and call its tools.
// Uses the official SDK client — proves the server actually speaks MCP end-to-end.
// Run: node --test server.test.mjs   (after `npm install`)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

async function connect() {
  const transport = new StdioClientTransport({ command: 'node', args: [join(here, 'server.mjs')] });
  const client = new Client({ name: 'capsim-test', version: '1.0.0' });
  await client.connect(transport);
  return client;
}

test('MCP: initialize + tools/list exposes exactly the 5 capsim tools, each with an inputSchema', async () => {
  const client = await connect();
  const { tools } = await client.listTools();
  const names = tools.map((t) => t.name).sort();
  assert.deepEqual(names, ['capsim_chaos', 'capsim_compare', 'capsim_cost', 'capsim_review', 'capsim_simulate']);
  for (const t of tools) {
    assert.ok(t.description && t.description.length > 10, `${t.name} described`);
    assert.ok(t.inputSchema && t.inputSchema.type === 'object', `${t.name} exposes inputSchema`);
  }
  await client.close();
});

test('MCP: invalid argument type is surfaced as a validation error (server robust)', async () => {
  const client = await connect();
  // The SDK validates against the zod inputSchema and returns an error RESULT (isError),
  // not a thrown rejection — the server never runs the handler on garbage input.
  const res = await client.callTool({ name: 'capsim_review', arguments: { tenants: 'lots' } });
  assert.equal(res.isError, true, 'invalid tenants must yield isError');
  assert.match(res.content[0].text, /validation|invalid/i);
  await client.close();
});

test('MCP: capsim_review returns the grounded scorecard (score 32 / CRITICAL at 50k)', async () => {
  const client = await connect();
  const res = await client.callTool({ name: 'capsim_review', arguments: { tenants: 50000 } });
  const payload = JSON.parse(res.content[0].text);
  assert.equal(payload.score, 32);
  assert.equal(payload.verdict, 'CRITICAL');
  assert.ok(payload.findings.some((f) => f.component === 'cell_count'), 'names cell_count');
  assert.ok(payload.findings.some((f) => f.component === 'single_region'), 'names single-region');
  await client.close();
});

test('MCP: multi-region inputs pass through — crossRegionReplication proven via capLossPct', async () => {
  const client = await connect();
  const chaosCall = (args) => client.callTool({ name: 'capsim_chaos', arguments: { ...args, mode: 'region' } }).then((r) => JSON.parse(r.content[0].text));
  // sharded (no replication) => 1/3 blast radius; active-active => 0. Distinct values PROVE the
  // crossRegionReplication flag reaches the engine (not silently stripped by the schema).
  const sharded = await chaosCall({ regions: 3, crossRegionReplication: false });
  const aa = await chaosCall({ regions: 3, crossRegionReplication: true });
  assert.ok(Math.abs(sharded.capLossPct - 1 / 3) < 1e-9, `sharded capLoss ${sharded.capLossPct} == 1/3`);
  assert.equal(aa.capLossPct, 0, 'active-active capLoss must be 0 (replication passthrough)');
  await client.close();
});

test('MCP: inputSchema advertises the multi-region + request-tier params', async () => {
  const client = await connect();
  const { tools } = await client.listTools();
  const review = tools.find((t) => t.name === 'capsim_review');
  const props = review.inputSchema.properties || {};
  assert.ok(props.regions, 'regions advertised');
  assert.ok(props.crossRegionReplication, 'crossRegionReplication advertised');
  assert.ok(props.bffReplicas, 'bffReplicas advertised');
  assert.ok(props.engineReplicas, 'engineReplicas advertised');
  await client.close();
});

test('MCP: bffReplicas passthrough — 6 has a critical BFF finding, 10 clears it', async () => {
  const client = await connect();
  const call = (args) => client.callTool({ name: 'capsim_review', arguments: args }).then((r) => JSON.parse(r.content[0].text));
  const def = await call({ tenants: 50000 });
  const tuned = await call({ tenants: 50000, bffReplicas: 10, engineReplicas: 11 });
  // If bffReplicas were stripped, both would be identical. It reaches the engine → findings differ.
  assert.ok(def.findings.some((f) => f.component === 'bff' && f.severity === 'critical'), 'default BFF critical');
  assert.ok(!tuned.findings.some((f) => f.component === 'bff'), 'bffReplicas 10 clears BFF (passthrough proven)');
  // engineReplicas passthrough: default has an engine_api finding, engineReplicas 11 clears it.
  assert.ok(def.findings.some((f) => f.component === 'engine_api'), 'default engine_api finding');
  assert.ok(!tuned.findings.some((f) => f.component === 'engine_api'), 'engineReplicas 11 clears engine_api (passthrough proven)');
  await client.close();
});

test('MCP: capsim_cost returns grounded monthly buckets (total == sum)', async () => {
  const client = await connect();
  const c = JSON.parse((await client.callTool({ name: 'capsim_cost', arguments: { tenants: 50000, regions: 1 } })).content[0].text);
  assert.ok(Math.abs(c.total - (c.compute + c.controlPlane + c.global)) < 1e-6, 'total == sum of buckets');
  assert.ok(c.total > 0 && c.cells === 100, `cells ${c.cells}`);
  await client.close();
});

test('MCP: capsim_compare returns the 3-posture tradeoff (active-active costs more, 0 blast)', async () => {
  const client = await connect();
  const t = JSON.parse((await client.callTool({ name: 'capsim_compare', arguments: { tenants: 50000, compareRegions: 3 } })).content[0].text);
  assert.equal(t.length, 3);
  assert.ok(t[2].costPerMonth > t[0].costPerMonth, 'active-active pricier than single');
  assert.equal(t[2].regionBlastRadiusPct, 0, 'active-active 0 blast');
  assert.ok(t[1].costOfResilienceVsSingle > 0, 'sharded has a resilience cost');
  await client.close();
});

test('MCP: capsim_simulate + capsim_chaos respond with structured data', async () => {
  const client = await connect();
  const sim = JSON.parse((await client.callTool({ name: 'capsim_simulate', arguments: { tenants: 50000 } })).content[0].text);
  assert.equal(sim.cells, 100);
  assert.equal(sim.bottleneck.id, 'cell_count');
  const region = JSON.parse((await client.callTool({ name: 'capsim_chaos', arguments: { tenants: 50000, mode: 'region' } })).content[0].text);
  assert.equal(region.capLossPct, 1);
  await client.close();
});

test('MCP: mgmt_nodes surfaces + couples — extreme bffReplicas raises mgmt nodes & cost (M8)', async () => {
  const client = await connect();
  const sim = (args) => client.callTool({ name: 'capsim_simulate', arguments: args }).then((r) => JSON.parse(r.content[0].text));
  const cst = (args) => client.callTool({ name: 'capsim_cost', arguments: args }).then((r) => JSON.parse(r.content[0].text));
  // mgmt_nodes is a first-class component through the MCP boundary
  const base = await sim({ tenants: 50000 });
  assert.ok(base.components.some((c) => c.id === 'mgmt_nodes'), 'mgmt_nodes component exposed via MCP');
  // coupling: at low tenants, cranking bffReplicas raises mgmt nodes (proves passthrough → packing)
  const calm = await cst({ tenants: 5000, regions: 1 });
  const cranked = await cst({ tenants: 5000, regions: 1, bffReplicas: 3000 });
  assert.ok(cranked.total > calm.total, `bff 3000 costs more (${cranked.total} > ${calm.total}) — replicas coupled to mgmt cost`);
  await client.close();
});

test('MCP: mode="cell" surfaces the cell-isolation blast (M9) — small blast, survivors untouched', async () => {
  const client = await connect();
  const chaosCall = (mode) => client.callTool({ name: 'capsim_chaos', arguments: { tenants: 50000, mode } }).then((r) => JSON.parse(r.content[0].text));
  const cell = await chaosCall('cell');
  const region = await chaosCall('region');
  // cell blast (~1%) is far smaller than a single-region blast (100%)
  assert.ok(Math.abs(cell.capLossPct - 0.01) < 1e-9, `cell blast ${cell.capLossPct} == 1%`);
  assert.equal(cell.cellTenants, 500, 'dead cell held 500 tenants');
  assert.equal(region.capLossPct, 1, 'single-region blast 100%');
  assert.ok(cell.capLossPct < region.capLossPct, 'cell isolation << region blast (the value prop)');
  // survivors untouched: the bff tier rho under cell chaos equals the no-chaos default
  const base = JSON.parse((await client.callTool({ name: 'capsim_simulate', arguments: { tenants: 50000 } })).content[0].text);
  const bffBase = base.components.find((c) => c.id === 'bff').rho;
  const bffCell = cell.components.find((c) => c.id === 'bff').rho;
  assert.ok(Math.abs(bffBase - bffCell) < 1e-9, 'survivors untouched (bff rho unchanged under killCell)');
  await client.close();
});
