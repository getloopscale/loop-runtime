// Tests for the LoopZero bottleneck simulation engine (loopzero-capsim.mjs).
// Run: node --test loopzero-capsim.test.mjs
// Evidence for M0 DoD #1. Pure, deterministic, no I/O.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { DEFAULTS, podsPerNode, simulate, chaos, scn, rdsMaxConns, rhoOf, review, cost, comparePostures, costBreakdown, mgmtNodes, subnetIps, buildPlane, traffic, wellArchitected, rdsFailoverSec, buildCost, PRICING_MULT, SPOT_FRACTION_OF_ONDEMAND, GATEWAY_NAT_REDUCTION_FRAC, PRICES, COST_QTY } from './loopzero-capsim.mjs';

const approx = (a, b, eps = 1e-6) => assert.ok(Math.abs(a - b) < eps, `${a} ~= ${b}`);

// ---- podsPerNode: the 3(+1) EKS density ceilings (grounded renderer.go:161-164) ----
test('podsPerNode: ENI-bound without prefix delegation (m8g.2xlarge)', () => {
  const p = podsPerNode(scn({ prefixDelegation: false }));
  assert.equal(p.cpu, 72);     // floor(8000*0.9 / 100m)
  assert.equal(p.mem, 230);    // floor(32*1024*0.9 / 128Mi)
  assert.equal(p.eni, 58);     // AWS eni-max-pods m8g.2xlarge
  assert.equal(p.kubelet, 110);
  assert.equal(p.ppn, 58);
  assert.equal(p.binding, 'eni');
});

test('podsPerNode: prefix delegation flips the binding ceiling to CPU', () => {
  const p = podsPerNode(scn({ prefixDelegation: true }));
  assert.equal(p.eni, 250);
  assert.equal(p.ppn, 72);     // now CPU-bound
  assert.equal(p.binding, 'cpu');
});

// ---- Layer A: packing (does 50k tenants FIT?) ----
test('packing: 50k tenants => 100 cells (maxPerCell=500, engine.go:91)', () => {
  const s = simulate(scn({ tenants: 50000 }));
  assert.equal(s.cells, 100);
  assert.equal(s.activePods, 4000);           // 50000 * 0.08
});

test('packing: node count depends on pod density (prefix delegation)', () => {
  assert.equal(simulate(scn({ prefixDelegation: false })).appNodes, 69); // ceil(4000/58)
  assert.equal(simulate(scn({ prefixDelegation: true })).appNodes, 56);  // ceil(4000/72)
});

// ---- Layer B: request queueing (does the control plane HANDLE the load?) ----
test('queueing: BFF saturates at 50k (rho > 1)', () => {
  const bff = simulate(scn()).components.find(c => c.id === 'bff');
  assert.ok(bff.rho > 1, `bff rho=${bff.rho}`);
  assert.equal(bff.saturatesAtTenants, 48000);  // replicas*mu / platformRpsPerTenant
});

// ---- Bottleneck ranking: the deliverable's core output ----
test('bottleneck: cell architecture is the top bottleneck at 50k', () => {
  const s = simulate(scn());
  assert.equal(s.bottleneck.id, 'cell_count');   // 100 cells > 64 operational ceiling
  assert.equal(s.bottleneck.saturatesAtTenants, 32000);
  // components are sorted by utilization desc
  for (let i = 1; i < s.components.length; i++)
    assert.ok(s.components[i - 1].rho >= s.components[i].rho, 'sorted desc');
});

test('no NaN/undefined anywhere in the simulation', () => {
  const s = simulate(scn());
  for (const c of s.components) {
    assert.ok(Number.isFinite(c.used), `${c.id}.used`);
    assert.ok(Number.isFinite(c.capacity), `${c.id}.capacity`);
    assert.ok(Number.isFinite(c.rho), `${c.id}.rho`);
  }
});

// ---- Chaos: kill an AZ, re-rank ----
test('chaos: killing 1 of 3 AZs loses ~1/3 capacity and worsens BFF', () => {
  const base = simulate(scn());
  const c = chaos(scn(), { killAZ: true });
  approx(c.capLossPct, 1 / 3, 1e-3);
  const bffBase = base.components.find(x => x.id === 'bff').rho;
  const bffChaos = c.components.find(x => x.id === 'bff').rho;
  assert.ok(bffChaos > bffBase, `chaos bff ${bffChaos} > base ${bffBase}`);
  assert.ok(c.sla > 0 && c.sla <= 100, `sla=${c.sla}`);
});

// ---- Boundary / negative ----
test('boundary: 0 tenants => no NaN, zero utilization', () => {
  const s = simulate(scn({ tenants: 0 }));
  assert.equal(s.cells, 0);
  assert.equal(s.activePods, 0);
  for (const c of s.components) assert.ok(Number.isFinite(c.rho) && c.rho >= 0);
});

test('DEFAULTS ground constants trace to code (documented)', () => {
  assert.equal(DEFAULTS.maxPerCell, 500);          // engine.go:91
  assert.equal(DEFAULTS.tenantPod.cpuM, 100);      // renderer.go:161
  assert.equal(DEFAULTS.tenantPod.memMi, 128);     // renderer.go:162
});

// ---- UI contract: every field the HTML (loopzero-bottleneck-simulator.html) reads MUST exist ----
// Guards against the static-HTML failure mode of hitting `undefined` at render time.
test('UI contract: engine exports are the 4 functions the HTML imports', () => {
  for (const fn of [scn, simulate, chaos, podsPerNode])
    assert.equal(typeof fn, 'function');
});

test('UI contract: simulate() + chaos() expose every field render() consumes', () => {
  const check = (R, isChaos) => {
    for (const k of ['cells', 'activePods', 'ppn', 'appNodes', 'sla', 'platformRps', 'agentRps'])
      assert.ok(Number.isFinite(R[k]), `R.${k}`);
    assert.ok(Array.isArray(R.components) && R.components.length >= 6, 'components[]');
    for (const c of R.components)
      for (const k of ['id', 'label', 'rho', 'used', 'capacity', 'unit', 'saturatesAtTenants', 'note'])
        assert.ok(c[k] !== undefined, `component.${k} (${c.id})`);
    for (const k of ['id', 'label', 'rho', 'saturatesAtTenants', 'note'])
      assert.ok(R.bottleneck[k] !== undefined, `bottleneck.${k}`);
    if (isChaos) assert.ok(Number.isFinite(R.capLossPct), 'capLossPct');
  };
  check(simulate(scn()), false);
  check(chaos(scn(), { killAZ: true }), true);
  check(chaos(scn(), { killRegion: true }), true);   // UI renders this mode too (ρ=∞ must not break render)
  check(chaos(scn(), { killCell: true }), true);     // M9: cell chaos mode renders too
  check(chaos(scn(), { killBuild: true }), true);    // M12: build-plane chaos (ρ=∞ must not break render)
  const p = podsPerNode(scn());
  assert.ok(Number.isFinite(p.ppn) && typeof p.binding === 'string', 'podsPerNode shape');
});

// ---- DoD #4 (strengthened): chaos CAN re-rank the top bottleneck ----
test('chaos re-ranks: a load-bound tier overtakes the AZ-independent cell_count', () => {
  const s = scn({ platformRpsPerTenant: 0.06 });   // bff base rho=1.25 < cell 1.5625
  const base = simulate(s);
  const c = chaos(s, { killAZ: true });             // bff chaos rho=1.875 > cell 1.5625
  assert.equal(base.bottleneck.id, 'cell_count');   // packing dominates at rest
  assert.equal(c.bottleneck.id, 'bff');             // losing an AZ flips the top to the request tier
});

// ---- LOW fix: guarded saturatesAtTenants (no NaN when a load term is 0) ----
test('div-by-zero guard: agentRps=0 => memory_rds saturatesAt is Infinity, never NaN', () => {
  const s = simulate(scn({ agentRpsPerActiveTenant: 0 }));
  const mem = s.components.find(x => x.id === 'memory_rds');
  assert.equal(mem.saturatesAtTenants, Infinity);
  for (const c of s.components) assert.ok(!Number.isNaN(c.saturatesAtTenants), `${c.id} not NaN`);
});

// ==================== M1 — calibrate + deepen ====================

// C1 — computed RDS max_connections (AWS Postgres formula), replaces the hardcoded 3600.
test('C1: rdsMaxConns computes the AWS formula per instance class', () => {
  assert.equal(rdsMaxConns(32), 3604);   // db.m8g.2xlarge
  assert.equal(rdsMaxConns(16), 1802);   // db.m8g.xlarge
  assert.equal(rdsMaxConns(8), 901);     // db.m8g.large
  assert.equal(rdsMaxConns(100000), 5000); // capped at 5000
});

// C2 — RDS Proxy multiplex scales read capacity linearly.
test('C2: proxyMaxConnPct scales RDS read capacity linearly', () => {
  const full = simulate(scn({ proxyMaxConnPct: 100 })).components.find(c => c.id === 'memory_rds').capacity;
  const half = simulate(scn({ proxyMaxConnPct: 50 })).components.find(c => c.id === 'memory_rds').capacity;
  assert.ok(Math.abs(full / 2 - half) <= 1, `${full}/2 ~= ${half}`);
});

// C3 — promptly (credential broker, db.m8g.large=901 conns) saturates before memory under agent load.
test('C3: promptly domain saturates before memory (per-class ceiling matters)', () => {
  const s = simulate(scn());
  const promptly = s.components.find(c => c.id === 'promptly_rds');
  const memory = s.components.find(c => c.id === 'memory_rds');
  assert.ok(promptly, 'promptly domain present');
  assert.ok(promptly.rho > memory.rho, `promptly ${promptly.rho} > memory ${memory.rho}`);
});

// C4 — rhoOf correctness: no capacity + demand = fully saturated (Infinity), not 0.
test('C4: rhoOf(used>0, cap=0) === Infinity; rhoOf(0,0) === 0', () => {
  assert.equal(rhoOf(1, 0), Infinity);
  assert.equal(rhoOf(0, 0), 0);
  assert.equal(rhoOf(50, 100), 0.5);
});

// C5 — region chaos: single-region target => TOTAL capacity loss (incl. the EKS clusters).
test('C5: chaos killRegion => capLossPct=1, blast radius total (every tier + cell_count down), SLA drops', () => {
  const base = simulate(scn());
  const c = chaos(scn(), { killRegion: true });
  assert.equal(c.capLossPct, 1);
  // MEDIUM-1 fix: the EKS clusters are IN the dead region — cell_count must be down, not "over-provisioned".
  assert.equal(c.components.find(x => x.id === 'cell_count').rho, Infinity);
  assert.ok(c.components.every(x => x.rho === Infinity), 'every component down under full-region loss');
  assert.ok(c.sla < base.sla, `region-outage sla ${c.sla} < base ${base.sla}`);
});

// LOW-2 (plan Test Plan): killRegion with 0 tenants — no NaN.
test('boundary: killRegion + 0 tenants => no NaN, DR sla intact', () => {
  const c = chaos(scn({ tenants: 0 }), { killRegion: true });
  assert.equal(c.sla, 95);                 // regionalOutageAvail default 0.95
  for (const x of c.components) assert.ok(!Number.isNaN(x.rho), `${x.id} rho not NaN`);
});

// LOW-3: regionalOutageAvail is clamped to [0,1] (no sla > 100).
test('LOW-3: regionalOutageAvail > 1 is clamped => sla capped at 100', () => {
  assert.equal(chaos(scn({ regionalOutageAvail: 1.5 }), { killRegion: true }).sla, 100);
});

// ==================== M2 — AI Design Review (rubric) ====================

// T1 — deterministic rubric: default (50k) is CRITICAL, names the real bottlenecks.
test('M2 review: default 50k scores CRITICAL and names cell_count + bff + single-region', () => {
  const r = review(scn());
  assert.equal(r.score, 32);          // 100 -20(cell) -20(bff) -8(engine warm) -10(engine az-fragile) -10(single-region)
  assert.equal(r.verdict, 'CRITICAL');
  const ids = r.findings.map(f => f.component);
  assert.ok(ids.includes('cell_count'), 'flags cell_count');
  assert.ok(ids.includes('bff'), 'flags bff');
  assert.ok(ids.includes('single_region'), 'flags single-region');
});

// T1 — monotonicity: a healthier design scores strictly higher.
test('M2 review: verdict monotonic — healthier design scores strictly higher (SOUND)', () => {
  const bad = review(scn()).score;
  const goodScn = scn({ tenants: 15000, prefixDelegation: true });
  assert.ok(review(goodScn).score > bad, `good ${review(goodScn).score} > bad ${bad}`);
  assert.equal(review(goodScn).verdict, 'SOUND');
});

// T1 — grounding: a finding's rho is the ENGINE's rho, not an invented number.
test('M2 review grounding: finding rho equals engine rho (no fabricated metric)', () => {
  const s = scn();
  const engineCell = simulate(s).components.find(c => c.id === 'cell_count').rho;
  const findingCell = review(s).findings.find(f => f.component === 'cell_count').rho;
  assert.equal(findingCell, engineCell);
});

// T1 — score clamped to [0,100], no NaN, even for a catastrophic design.
test('M2 review: score clamped to [0,100], never NaN', () => {
  const r = review(scn({ tenants: 500000, bff: { replicas: 1 }, cellSoftCeiling: 4 }));
  assert.ok(r.score >= 0 && r.score <= 100, `score ${r.score}`);
  assert.ok(!Number.isNaN(r.score));
  assert.ok(['SOUND', 'NEEDS_REVISION', 'AT_RISK', 'CRITICAL'].includes(r.verdict));
});

// L1 fix — real monotonicity: score never RISES as load grows (not just one example).
test('M2 review: score is monotonically non-increasing across a tenant sweep', () => {
  let prev = 101;
  for (let t = 1000; t <= 120000; t += 5000) {
    const sc = review(scn({ tenants: t })).score;
    assert.ok(sc <= prev, `tenants=${t}: ${sc} must be <= ${prev}`);
    prev = sc;
  }
});

// L3 fix — az-loss findings are grounded on chaos(killAZ), and `basis` disambiguates base vs az ρ.
test('M2 review grounding: az-loss finding rho equals chaos(killAZ) rho; basis field distinguishes it', () => {
  const s = scn();
  const azRho = chaos(s, { killAZ: true }).components.find((c) => c.id === 'engine_api').rho;
  const f = review(s).findings.find((x) => x.component === 'engine_api' && x.basis === 'az-loss');
  assert.ok(f, 'az-loss finding present');
  assert.equal(f.rho, azRho);
  // and the base finding for the same component carries the base ρ, not the az ρ
  const b = review(s).findings.find((x) => x.component === 'engine_api' && x.basis === 'base');
  assert.equal(b.rho, simulate(s).components.find((c) => c.id === 'engine_api').rho);
});

// ==================== M3 — multi-region + failover ====================

// no-regression: default regions=1 (replication irrelevant) => single-region total loss.
test('M3: regions=1 => region kill is a total outage (replication flag irrelevant)', () => {
  const c = chaos(scn({ regions: 1, crossRegionReplication: true }), { killRegion: true });
  assert.equal(c.capLossPct, 1);
  assert.equal(c.sla, 95);   // DR floor
});

// sharded: survivors run at base ρ; blast radius = 1/regions; SLA blended (between DR floor and full).
test('M3 sharded: regions=3 no replication => capLoss=1/3, survivors ρ unchanged, SLA blended', () => {
  const s = scn({ regions: 3, crossRegionReplication: false });
  const base = simulate(s);
  const c = chaos(s, { killRegion: true });
  approx(c.capLossPct, 1 / 3, 1e-9);
  assert.equal(c.components.find((x) => x.id === 'bff').rho, base.components.find((x) => x.id === 'bff').rho);
  assert.ok(c.sla > 95 && c.sla < base.sla, `blended ${c.sla} in (95, ${base.sla})`);
});

// active-active: 0 tenants lost, but survivors absorb failover load (ρ ×= regions/(regions-1)).
test('M3 active-active: regions=3 replication => capLoss=0, survivors ρ ×1.5, SLA > sharded', () => {
  const s = scn({ regions: 3, crossRegionReplication: true });
  const base = simulate(s);
  const aa = chaos(s, { killRegion: true });
  const sharded = chaos(scn({ regions: 3, crossRegionReplication: false }), { killRegion: true });
  assert.equal(aa.capLossPct, 0);
  approx(aa.components.find((x) => x.id === 'bff').rho, base.components.find((x) => x.id === 'bff').rho * 1.5, 1e-6);
  assert.ok(aa.sla > sharded.sla, `aa ${aa.sla} > sharded ${sharded.sla}`);
});

// review posture: resilience gain (WITH headroom) active-active > sharded > single-region.
test('M3 review: with headroom, active-active scores > sharded > single-region', () => {
  const headroom = { tenants: 15000, prefixDelegation: true };
  const single = review(scn({ ...headroom, regions: 1 })).score;
  const sharded = review(scn({ ...headroom, regions: 3, crossRegionReplication: false })).score;
  const aa = review(scn({ ...headroom, regions: 3, crossRegionReplication: true })).score;
  assert.ok(aa > sharded && sharded > single, `aa ${aa} > sharded ${sharded} > single ${single}`);
});

// honesty: at high load with thin replicas, active-active surfaces region-fragile (no overselling).
test('M3 review: high-load active-active surfaces a region-fragile finding (survivors saturate)', () => {
  const r = review(scn({ regions: 3, crossRegionReplication: true }));   // 50k default load
  assert.ok(r.findings.some((f) => f.basis === 'region-loss'), 'flags a region-fragile tier');
});

// H-1 fix — resilience-ranking invariant: a 0%-blast-radius posture NEVER scores below a
// total-outage one, even with multiple region-fragile tiers (uncapped, this inverted).
test('M3 review invariant: active-active (0% blast) never scores below single-region (100% blast)', () => {
  // {platformRpsPerTenant:0.045} makes bff+engine BOTH region-fragile under active-active — the
  // exact 2-fragile-tier case that inverted the ranking before the cap.
  for (const ov of [{}, { tenants: 80000 }, { platformRpsPerTenant: 0.045 }]) {
    const single = review(scn({ ...ov, regions: 1 })).score;
    const aa = review(scn({ ...ov, regions: 3, crossRegionReplication: true })).score;
    assert.ok(aa >= single, `aa ${aa} >= single ${single} for ${JSON.stringify(ov)}`);
  }
});

// ==================== M4 — cost-coupled review ====================

// M5 — the calculator is now ENGINE-BACKED: it imports loopzero-capsim.mjs and computes via cost()/
// costBreakdown(), so it can no longer hold an independent (divergent) price table. The M4
// two-source keyed guard is thereby obsolete — replaced by this single-source structural test.
test('M5 single-source: Calculator component imports the engine and defines no independent cost table', (t) => {
  const url = new URL('../components/Calculator.tsx', import.meta.url);
  // The Calculator lives only in the React app; the synced MCP-engine copy of this test
  // runs where it is absent — skip there, enforce the invariant where the file exists.
  if (!existsSync(url)) { t.skip('Calculator.tsx not co-located (MCP-engine copy)'); return; }
  const src = readFileSync(url, 'utf8');
  assert.match(src, /from\s*['"]\.\.\/lib\/capsim(\.mjs)?['"]/, 'calc must import the engine module');
  assert.match(src, /\bcostBreakdown\s*\(/, 'calc must render the engine costBreakdown()');
  assert.match(src, /\bcost\s*\(/, 'calc must compute the total via engine cost()');
  assert.doesNotMatch(src, /const\s+ITEMS\s*=/, 'calc must NOT define its own ITEMS price table');
  assert.doesNotMatch(src, /const\s+PRICES\s*=/, 'calc must NOT define its own PRICES table');
});

// costBreakdown line items sum EXACTLY to cost().total — the identity that makes the calculator's
// itemized view and the engine's bucketed total the same number.
test('M5 costBreakdown: subtotals sum to cost().total (single + multi-region postures)', () => {
  for (const ov of [{ regions: 1 }, { regions: 3, crossRegionReplication: false }, { regions: 3, crossRegionReplication: true }]) {
    const s = scn(ov);
    const sum = costBreakdown(s).reduce((a, i) => a + i.subtotal, 0);
    approx(sum, cost(s).total, 1e-6);
  }
});

test('M5 costBreakdown: default sums to the canonical single-region total ($76,656)', () => {
  const sum = costBreakdown(scn()).reduce((a, i) => a + i.subtotal, 0);
  assert.equal(Math.round(sum), 76656);
});

// ==================== M7 — request-tier replicas exposed (flat aliases) ====================
test('M7 scn aliases: bffReplicas/engineReplicas map to nested; flat wins; alias keys stripped', () => {
  const a = scn({ bffReplicas: 7, engineReplicas: 11 });
  assert.equal(a.bff.replicas, 7);
  assert.equal(a.engineApi.replicas, 11);
  assert.equal(a.bffReplicas, undefined, 'flat alias key stripped');
  assert.equal(a.engineReplicas, undefined, 'flat alias key stripped');
  const b = scn({ bff: { replicas: 3 }, bffReplicas: 9 });   // flat wins over nested
  assert.equal(b.bff.replicas, 9);
  assert.equal(b.bff.muRps, DEFAULTS.bff.muRps, 'nested siblings preserved');
});

test('M7 request tier: bffReplicas 6→7 resolves BFF; engineReplicas 8→11 resolves AZ-fragility', () => {
  assert.ok(simulate(scn()).components.find((c) => c.id === 'bff').rho > 1, 'default BFF saturated');
  assert.ok(simulate(scn({ bffReplicas: 7 })).components.find((c) => c.id === 'bff').rho < 1, 'bffReplicas 7 resolves');
  const azDefault = chaos(scn(), { killAZ: true }).components.find((c) => c.id === 'engine_api').rho;
  const az11 = chaos(scn({ engineReplicas: 11 }), { killAZ: true }).components.find((c) => c.id === 'engine_api').rho;
  assert.ok(azDefault >= 1, 'default engine AZ-fragile');
  assert.ok(az11 < 1, 'engineReplicas 11 → AZ-safe');
});

test('M7 review: bffReplicas 7 resolves BFF saturation (→warm); 10 fully clears (base+AZ); engineReplicas 11 clears engine', () => {
  // 7 resolves saturation (ρ<1) but leaves BFF WARM (0.8≤ρ<1) — still a (high) finding.
  const r7 = review(scn({ bffReplicas: 7 })).findings.find((f) => f.component === 'bff');
  assert.equal(r7.severity, 'high', 'BFF at 7 is warm, not critical');
  // 8 clears the base saturation but BFF becomes AZ-fragile (az-loss finding); 10 fully clears.
  assert.ok(review(scn({ bffReplicas: 8 })).findings.some((f) => f.component === 'bff' && f.basis === 'az-loss'), 'BFF at 8 is AZ-fragile');
  const r = review(scn({ bffReplicas: 10, engineReplicas: 11 }));
  assert.ok(!r.findings.some((f) => f.component === 'bff'), 'BFF fully cleared (base+AZ) at 10');
  assert.ok(!r.findings.some((f) => f.component === 'engine_api'), 'engine_api cleared at 11');
});

// canonical source-of-truth lock: ANY silent edit to a price/quantity (add/remove/change) fails.
// Full deepEqual — not a named subset — so every one of the ~20 prices + ~19 quantities is guarded.
test('M5 canonical lock: PRICES + COST_QTY hold their exact canonical (Vantage RI) values', () => {
  assert.deepEqual(PRICES, {
    ec2Node: 173, eksCluster: 73, rdsMem: 160, rdsRag: 160, rdsLens: 80, rdsProm: 40, rdsTrust: 40,
    rdsProxy: 50, cacheNode: 128, rdsStoreGB: 0.115, s3GB: 0.023, ecrGB: 0.10, cfEgressGB: 0.085,
    cfReqM: 0.75, natGB: 0.045, albMonth: 60, natGW: 33, waf: 1300, r53Zone: 0.90, kmsKey: 1.10, smSecret: 0.45,
    dtCrossAzGB: 0.01, dtInterRegionGB: 0.02,   // M13 data transfer
  });
  assert.deepEqual(COST_QTY, {
    rdsMemN: 4, rdsRagN: 4, rdsLensN: 4, rdsPromN: 3, rdsTrustN: 3, rdsProxyN: 6, cacheN: 3,
    rdsStoreGB: 4000, s3GB: 15000, ecrGB: 60000, cfEgressGB: 50000, cfReqM: 2000, natGBn: 5000,
    albN: 2, natN: 3, wafN: 1, r53N: 8, kmsN: 8, smN: 30,
  });
});

// UI contract (M4): every cost/compare field the HTML reads must exist (no render-time undefined).
test('M4 UI contract: cost() + comparePostures() expose the fields the HTML consumes', () => {
  const c = cost(scn());
  for (const k of ['total', 'compute', 'controlPlane', 'global', 'totalNodes', 'cells'])
    assert.ok(Number.isFinite(c[k]), `cost.${k}`);
  const rows = comparePostures(scn(), { regions: 3 });
  for (const r of rows)
    for (const k of ['posture', 'costPerMonth', 'regionBlastRadiusPct', 'sla', 'reviewScore', 'costOfResilienceVsSingle'])
      assert.ok(r[k] !== undefined, `row.${k}`);
});

// cost buckets: total = compute + controlPlane + global at single-region.
test('M4 cost: total == compute + controlPlane + global (single region), all finite', () => {
  const c = cost(scn({ regions: 1 }));
  approx(c.total, c.compute + c.controlPlane + c.global, 1e-6);
  for (const k of ['total', 'compute', 'controlPlane', 'global']) assert.ok(Number.isFinite(c[k]), k);
});

// posture cost ordering + EXACT resilience deltas.
test('M4 cost posture: single < sharded < active-active; Δs are exact (controlPlane / compute)', () => {
  const single = cost(scn({ tenants: 50000, regions: 1 })).total;
  const sharded = cost(scn({ tenants: 50000, regions: 3, crossRegionReplication: false }));
  const aa = cost(scn({ tenants: 50000, regions: 3, crossRegionReplication: true }));
  assert.ok(single < sharded.total && sharded.total < aa.total, `${single} < ${sharded.total} < ${aa.total}`);
  approx(sharded.total - single, sharded.controlPlane * 2, 1e-2);   // sharded adds control-plane ×(R-1)
  // active-active adds compute ×(R-1) PLUS the M13 inter-region data-transfer bucket
  const interRegion = PRICES.dtInterRegionGB * traffic(scn({ tenants: 50000, regions: 3, crossRegionReplication: true })).interRegionGB;
  approx(aa.total - sharded.total, sharded.compute * 2 + interRegion, 1e-2);
});

// comparePostures tradeoff table.
test('M4 comparePostures: 3-row table with cost-of-resilience + blast radius', () => {
  const t = comparePostures(scn(), { regions: 3 });
  assert.deepEqual(t.map((r) => r.posture), ['single', 'sharded', 'active-active']);
  assert.equal(t[0].costOfResilienceVsSingle, 0);
  assert.ok(t[1].costOfResilienceVsSingle > 0 && t[2].costOfResilienceVsSingle > t[1].costOfResilienceVsSingle);
  assert.equal(t[0].regionBlastRadiusPct, 1);   // single = total loss
  assert.equal(t[2].regionBlastRadiusPct, 0);   // active-active = 0 blast
});

// grounding: review's cost metric is the cost() total (no independent number).
test('M4 review: metrics.costPerMonth == round(cost(scenario).total)', () => {
  const s = scn();
  assert.equal(review(s).metrics.costPerMonth, Math.round(cost(s).total));
});

// posture findings + grounding.
test('M3 review posture: single→single_region(-10), sharded→region_sharded(-5), grounded', () => {
  const single = review(scn({ regions: 1 }));
  const sharded = review(scn({ tenants: 15000, prefixDelegation: true, regions: 3 }));
  assert.ok(single.findings.some((f) => f.component === 'single_region'));
  const shf = sharded.findings.find((f) => f.component === 'region_sharded');
  assert.ok(shf, 'sharded posture finding present');
  approx(shf.capLossPct, chaos(scn({ tenants: 15000, prefixDelegation: true, regions: 3 }), { killRegion: true }).capLossPct, 1e-9);
});

// C6 — regionalOutageAvail drives the region-outage SLA floor (labeled POLICY).
test('C6: regionalOutageAvail sets the region-outage SLA', () => {
  const c = chaos(scn({ regionalOutageAvail: 0.90 }), { killRegion: true });
  assert.equal(c.sla, 90);
});

// C7 — calibration sensitivity: doubling per-replica throughput halves the tier utilization.
test('C7: sensitivity — 2x bff muRps halves bff rho (model is calibratable)', () => {
  const base = simulate(scn()).components.find(c => c.id === 'bff').rho;
  const dbl = simulate(scn({ bff: { muRps: 800 } })).components.find(c => c.id === 'bff').rho;
  assert.ok(Math.abs(base / 2 - dbl) < 1e-9, `${base}/2 ~= ${dbl}`);
});

// ---- M8: mgmt / control-plane cluster capacity (BFF+Engine replicas couple to mgmt nodes) ----

// M8-1 — mgmtNodes at default is floor-bound (10): tiny BFF/Engine pods pack below the HA floor.
test('M8: mgmtNodes default is floor-bound at fixedNodes (10) — $76,656 anchor preserved', () => {
  const m = mgmtNodes(scn());
  assert.equal(m.nodes, 10, 'default mgmt nodes = HA floor 10');
  assert.ok(m.packed < 10, `packed ${m.packed} < floor (floor dominates)`);
  assert.equal(m.floorBound, true, 'floorBound flag true at default');
});

// M8-2 — packing math grounded in chart requests (bff 50m/64Mi, eng 100m/64Mi) + platform baseline.
test('M8: mgmtNodes packing = platform baseline + replicas x per-pod requests', () => {
  const s = scn();
  const m = mgmtNodes(s);
  const allocCpuM = s.node.vcpu * 1000 * (1 - s.node.reservePct);      // 7200
  const allocMemMi = s.node.memGiB * 1024 * (1 - s.node.reservePct);   // 29491.2
  assert.equal(m.cpuM, s.mgmt.platformCpuM + s.bff.replicas * s.bff.cpuM + s.engineApi.replicas * s.engineApi.cpuM);
  assert.equal(m.memMi, s.mgmt.platformMemMi + s.bff.replicas * s.bff.memMi + s.engineApi.replicas * s.engineApi.memMi);
  const packed = Math.max(Math.ceil(m.cpuM / allocCpuM), Math.ceil(m.memMi / allocMemMi));
  assert.equal(m.packed, packed, 'packed = max(cpu-bound, mem-bound)');
});

// M8-3 — coupling: extreme BFF replicas EXCEED the floor → mgmt nodes rise (fiction removed).
test('M8: extreme bffReplicas exceed the HA floor — mgmt nodes rise (coupled)', () => {
  const base = mgmtNodes(scn()).nodes;                       // 10
  const big = mgmtNodes(scn({ bffReplicas: 2000 }));         // 2000*50m + baseline -> >72000m -> >10 nodes
  assert.ok(big.nodes > base, `bff 2000 -> ${big.nodes} nodes > floor ${base}`);
  assert.equal(big.floorBound, false, 'no longer floor-bound at 2000 replicas');
  assert.equal(big.binding, 'cpu', '2000 tiny pods are cpu-bound');
});

// M8-4 — cost() default anchor regression guard (mgmt derivation does not independently move $76,656).
test('M8: cost() default holds the M13 anchor ($76,656.30) — mgmt derivation invariant', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
  assert.equal(cost(scn()).totalNodes, cost(scn()).totalNodes); // stable
});

// M8-5 — cost() NOW couples to replicas at scale (M7 said invariant; M8 makes extreme replicas cost).
test('M8: cost() rises when replicas exceed the mgmt floor (replicas no longer free at scale)', () => {
  const base = cost(scn()).total;
  const scaled = cost(scn({ bffReplicas: 2000 })).total;
  const nodesDelta = mgmtNodes(scn({ bffReplicas: 2000 })).nodes - mgmtNodes(scn()).nodes;
  assert.ok(scaled > base, `scaled ${scaled} > base ${base}`);
  // extra EC2 nodes AND their coupled NAT-GB processing (M13: natGB = totalNodes × natGBPerNodeMonth)
  const perNode = PRICES.ec2Node + DEFAULTS.natGBPerNodeMonth * PRICES.natGB;
  approx(scaled - base, nodesDelta * perNode, 1e-2);
  // and small bumps within the floor stay free (honest: tiny pods absorbed by HA headroom)
  assert.equal(cost(scn({ bffReplicas: 10, engineReplicas: 11 })).total, base, 'small bump absorbed by floor');
});

// M8-6 — simulate() surfaces the mgmt_nodes bottleneck component with correct rho.
test('M8: simulate() exposes mgmt_nodes component (used=mgmtNodes, cap=nodeSoftCeiling)', () => {
  const s = scn();
  const comp = simulate(s).components.find((c) => c.id === 'mgmt_nodes');
  assert.ok(comp, 'mgmt_nodes component present');
  assert.equal(comp.used, mgmtNodes(s).nodes);
  assert.equal(comp.capacity, Math.round(s.mgmt.nodeSoftCeiling));
  approx(comp.rho, mgmtNodes(s).nodes / s.mgmt.nodeSoftCeiling, 1e-9);
});

// M8-7 — mgmt_nodes becomes the top bottleneck when replicas are cranked past the node ceiling.
test('M8: mgmt_nodes bottleneck fires (top) when replicas blow past nodeSoftCeiling', () => {
  // low tenants (cell_count healthy) isolates the mgmt tier: 5000 tiny 50m pods pack to ~39 nodes >> 30 ceiling
  const s = scn({ tenants: 5000, bffReplicas: 5000 });
  const top = simulate(s).bottleneck;
  assert.equal(top.id, 'mgmt_nodes', `top bottleneck is mgmt_nodes (got ${top.id})`);
  assert.ok(top.rho > 1, `saturated rho ${top.rho} > 1`);
});

// ---- M9: intra-cell blast radius (killCell) — quantifies the cell-isolation value prop ----

// M9-1 — one cell down loses cellTenants/total (~1% at 50k), the cell-isolation blast radius.
test('M9: killCell blast = cellTenants/total (1% at 50k, 100 cells)', () => {
  const c = chaos(scn(), { killCell: true });
  approx(c.capLossPct, 0.01, 1e-9);          // 500/50000
  assert.equal(c.cellTenants, 500);
});

// M9-2 — ISOLATION: survivors are UNTOUCHED (survivorFactor=1), unlike killAZ which drops capacity.
test('M9: survivors untouched under killCell (base rho preserved — sticky isolation)', () => {
  const base = simulate(scn());
  const cell = chaos(scn(), { killCell: true });
  for (const id of ['bff', 'engine_api', 'memory_rds', 'cell_count', 'cache_mem']) {
    const b = base.components.find(x => x.id === id).rho;
    const k = cell.components.find(x => x.id === id).rho;
    assert.ok(Math.abs(b - k) < 1e-9, `${id}: killCell rho ${k} == base ${b} (no failover load)`);
  }
  // contrast: killAZ DOES worsen a load tier
  const az = chaos(scn(), { killAZ: true });
  assert.ok(az.components.find(x => x.id === 'bff').rho > base.components.find(x => x.id === 'bff').rho, 'killAZ worsens bff (proves killCell isolation is distinct)');
});

// M9-3 — SLA blended per-tenant: (1-frac) at full, frac at DR floor.
test('M9: killCell SLA is blended per-tenant', () => {
  const full = simulate(scn()).sla;
  const drFloor = scn().regionalOutageAvail * 100;   // 95
  const frac = 0.01;
  const cell = chaos(scn(), { killCell: true });
  approx(cell.sla, Math.round(((1 - frac) * full + frac * drFloor) * 1e4) / 1e4, 1e-3);
});

// M9-4 — THE VALUE PROP: a cell failure blast (1%) is far smaller than a single-region failure (100%).
test('M9: cell isolation << single-region blast (1% vs 100%)', () => {
  const cell = chaos(scn({ regions: 1 }), { killCell: true });
  const region = chaos(scn({ regions: 1 }), { killRegion: true });
  assert.ok(cell.capLossPct < region.capLossPct, `cell ${cell.capLossPct} < region ${region.capLossPct}`);
  assert.equal(region.capLossPct, 1);
  assert.ok(cell.capLossPct <= 0.01 + 1e-9, 'cell blast capped ~1% at 50k');
});

// M9-5 — cellTenants is capped at maxPerCell; a single-cell deployment loses everyone.
test('M9: cellTenants capped at maxPerCell; 1-cell deployment => 100% blast', () => {
  assert.equal(chaos(scn({ tenants: 300 }), { killCell: true }).cellTenants, 300);  // 1 cell, all tenants
  approx(chaos(scn({ tenants: 300 }), { killCell: true }).capLossPct, 1, 1e-9);
  assert.equal(chaos(scn({ tenants: 100000 }), { killCell: true }).cellTenants, 500); // capped at maxPerCell
});

// M9-6 — killCell does not change cell count or app packing (it's a chaos snapshot, not a resize).
test('M9: killCell leaves cells + appNodes unchanged (snapshot, not resize)', () => {
  const base = simulate(scn());
  const cell = chaos(scn(), { killCell: true });
  assert.equal(cell.cells, base.cells);
  assert.equal(cell.appNodes, base.appNodes);
  // cost() is a function of the scenario only — chaos is a read-time snapshot, so cost is unchanged.
  approx(cost(scn()).total, 76656.2994, 1e-2);
});

// M9-7 — boundary: 0 tenants => no NaN, zero blast, zero cellTenants.
test('M9: boundary tenants=0 => killCell capLoss 0, cellTenants 0, no NaN', () => {
  const c = chaos(scn({ tenants: 0 }), { killCell: true });
  assert.equal(c.capLossPct, 0);
  assert.equal(c.cellTenants, 0);
  assert.ok(Number.isFinite(c.sla) && !Number.isNaN(c.sla), 'sla finite');
  for (const x of c.components) assert.ok(!Number.isNaN(x.rho), `${x.id} rho not NaN`);
});

// ==================== M11 — network capacity (subnet IP exhaustion) ====================

// M11-1 — subnet IP math: /20 = 4091 usable × 3 AZ subnets; secondary-IP mode by default.
test('M11: subnetIps computes usable IPs, capacity, and secondary-IP consumption', () => {
  const ni = subnetIps(scn());
  assert.equal(ni.usablePerSubnet, 4091);       // 2^12 - 5 reserved
  assert.equal(ni.appSubnets, 3);               // one private-app subnet per AZ
  assert.equal(ni.capacity, 12273);             // 4091 × 3
  assert.equal(ni.mode, 'secondary');
  assert.equal(ni.podIps, 4000);                // 50000 × 0.08 × 1 (1 IP/pod)
  approx(ni.used, 4000 + subnetIps(scn()).warm, 1e-9);
});

// M11-2 — the subnet_ips component surfaces with the right ρ (≈0.42 at 50k default — NOT the top).
test('M11: simulate() exposes subnet_ips (ρ≈0.42 default; not saturated)', () => {
  const c = simulate(scn()).components.find((x) => x.id === 'subnet_ips');
  assert.ok(c, 'subnet_ips component present');
  const ni = subnetIps(scn());
  assert.equal(c.used, ni.used);
  assert.equal(c.capacity, ni.capacity);
  approx(c.rho, ni.used / ni.capacity, 1e-9);
  assert.ok(c.rho > 0.3 && c.rho < 0.6, `default rho ${c.rho}`);
});

// M11-3 — prefix delegation packs pod IPs into /28 (16-IP) prefixes.
test('M11: prefix delegation rounds pod IPs to /28 prefixes', () => {
  const ni = subnetIps(scn({ prefixDelegation: true }));
  assert.equal(ni.mode, 'prefix');
  assert.equal(ni.podIps, Math.ceil(4000 / 16) * 16);   // /28 granularity
});

// M11-4 — custom networking (100.64/10) removes pod IP pressure from the VPC /20 → ρ collapses.
test('M11: custom networking zeroes pod IPs on the app subnet — ρ collapses', () => {
  const ni = subnetIps(scn({ customNetworking: true }));
  assert.equal(ni.mode, 'custom-networking');
  assert.equal(ni.podIps, 0);
  // residual /20 pressure is ONLY node primary-ENI IPs (≈1/node), not the full warm pool
  assert.equal(ni.warm, 69);              // ceil(4000/58) app nodes × 1 primary IP
  assert.equal(ni.used, 69);              // podIps(0) + node primary IPs
  const secRho = simulate(scn()).components.find((x) => x.id === 'subnet_ips').rho;
  const cusRho = simulate(scn({ customNetworking: true })).components.find((x) => x.id === 'subnet_ips').rho;
  assert.ok(cusRho < secRho * 0.4, `custom ${cusRho} ≪ secondary ${secRho}`);
});

// M11-5 — subnet IP exhaustion becomes the TOP bottleneck at honest scale (healthy cells isolate it).
test('M11: subnet_ips is the top bottleneck at 200k when cells are healthy (secondary mode)', () => {
  // healthy cells (maxPerCell 5000 → 40/64) + healthy request tier (scaled replicas) isolates the IP wall
  const iso = { tenants: 200000, maxPerCell: 5000, bffReplicas: 40, engineReplicas: 40 };
  const top = simulate(scn(iso)).bottleneck;
  assert.equal(top.id, 'subnet_ips', `top ${top.id}`);
  assert.ok(top.rho > 1, `exhausted ρ ${top.rho}`);
  // custom networking clears the exhaustion
  assert.ok(simulate(scn({ ...iso, customNetworking: true })).components.find((x) => x.id === 'subnet_ips').rho < 1, 'custom networking clears exhaustion');
});

// M11-6 — AZ loss drops an app subnet → less IP capacity → worse subnet_ips ρ.
test('M11: killAZ worsens subnet_ips ρ (capacity falls with the lost AZ subnet)', () => {
  const base = simulate(scn()).components.find((x) => x.id === 'subnet_ips').rho;
  const az = chaos(scn(), { killAZ: true }).components.find((x) => x.id === 'subnet_ips').rho;
  assert.ok(az > base, `killAZ ${az} > base ${base}`);
});

// M11-7 — anchor preserved: subnet_ips is a capacity model, not priced.
test('M11: cost() anchor holds ($76,656.30) — subnet_ips is not priced', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
});

// M11-8 — boundary: 0 tenants => no NaN, zero pod IPs.
test('M11: boundary tenants=0 => subnet_ips finite, zero pod IPs', () => {
  const ni = subnetIps(scn({ tenants: 0 }));
  assert.equal(ni.podIps, 0);
  const c = simulate(scn({ tenants: 0 })).components.find((x) => x.id === 'subnet_ips');
  assert.ok(Number.isFinite(c.rho) && c.rho >= 0, `rho ${c.rho}`);
  assert.ok(!Number.isNaN(c.saturatesAtTenants), 'sat not NaN');
});

// ==================== M12 — three-cluster separation (the build plane) ====================

// M12-1 — build math: 3 builds/node (CPU-bound), 20-node ceiling → 60 concurrent; Little's-law demand.
test('M12: buildPlane computes builds/node, capacity, and Little’s-law demand', () => {
  const bp = buildPlane(scn());
  assert.equal(bp.buildsPerNode, 3);        // min(floor(7200/2000), floor(29491/4096)) = min(3,7)
  assert.equal(bp.buildSoftCeiling, 20);
  assert.equal(bp.capacity, 60);            // 20 × 3
  approx(bp.concurrentBuilds, 4000 * 2 / 86400 * 240, 1e-9);  // 22.22 (activeTenants × deploys/day × dur / 86400)
});

// M12-2 — build_concurrency component present, ρ≈0.37 at default (NOT the top; cell_count 1.56 stays top).
test('M12: simulate() exposes build_concurrency (ρ≈0.37 default; cell_count still top)', () => {
  const s = simulate(scn());
  const c = s.components.find((x) => x.id === 'build_concurrency');
  assert.ok(c, 'build_concurrency present');
  approx(c.rho, buildPlane(scn()).concurrentBuilds / 60, 1e-9);
  assert.ok(c.rho > 0.3 && c.rho < 0.45, `default rho ${c.rho}`);
  assert.equal(s.bottleneck.id, 'cell_count');   // build plane does not displace the top at default
});

// M12-3 — a deploy burst makes the build plane the top bottleneck.
test('M12: deploy burst saturates the build plane (top bottleneck, ρ>1)', () => {
  const top = simulate(scn({ deploysPerActiveTenantPerDay: 20 })).bottleneck;
  assert.equal(top.id, 'build_concurrency', `top ${top.id}`);
  assert.ok(top.rho > 1, `saturated ρ ${top.rho}`);
});

// M12-4 — PLANE ISOLATION: killing the build plane blocks deploys but serving is untouched.
test('M12: killBuild blocks deploys (build ρ=∞) while serving (bff) is untouched', () => {
  const base = simulate(scn());
  const kb = chaos(scn(), { killBuild: true });
  const bc = kb.components.find((x) => x.id === 'build_concurrency');
  assert.equal(bc.capacity, 0, 'build capacity zeroed');
  assert.equal(bc.rho, Infinity, 'build ρ = ∞ (deploys blocked)');
  assert.equal(kb.bottleneck.id, 'build_concurrency', 'build plane is the top under killBuild');
  // serving is untouched: request/data tiers keep their base ρ
  for (const id of ['bff', 'engine_api', 'cell_count', 'memory_rds']) {
    const b = base.components.find((x) => x.id === id).rho;
    const k = kb.components.find((x) => x.id === id).rho;
    assert.ok(Math.abs(b - k) < 1e-9, `${id} untouched (${k} == ${b})`);
  }
  assert.equal(kb.capLossPct, 0, 'no serving capacity lost');
  assert.equal(kb.deploysBlocked, true, 'deploysBlocked flagged');
  assert.equal(kb.sla, base.sla, 'SLA unchanged (serving intact)');
});

// M12-5 — AZ loss reduces the build cluster's nodes → less build capacity.
test('M12: killAZ reduces build capacity (worse build_concurrency ρ)', () => {
  const base = simulate(scn()).components.find((x) => x.id === 'build_concurrency').rho;
  const az = chaos(scn(), { killAZ: true }).components.find((x) => x.id === 'build_concurrency').rho;
  assert.ok(az > base, `killAZ ${az} > base ${base}`);
});

// M12-6 — anchor preserved: build plane is a capacity model (M13 owns cost).
test('M12: cost() anchor holds ($76,656.30) — build plane not priced in M12', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
});

// M12-7 — boundary: 0 tenants => 0 concurrent builds, no NaN.
test('M12: boundary tenants=0 => zero concurrent builds, no NaN', () => {
  assert.equal(buildPlane(scn({ tenants: 0 })).concurrentBuilds, 0);
  const c = simulate(scn({ tenants: 0 })).components.find((x) => x.id === 'build_concurrency');
  assert.ok(Number.isFinite(c.rho) && c.rho >= 0, `rho ${c.rho}`);
});

// ==================== M13 — edge + LB + data-transfer cost completeness ====================

const NEW_ANCHOR = 76656.2994;   // M13 deliberately moved it from 72565.5 (see docs/m13-plan.md)

// M13-1 — CloudFront requests/egress derived from RPS (was fixed COST_QTY).
test('M13: traffic derives CloudFront requests + egress from platform RPS', () => {
  const t = traffic(scn());
  const SEC = 2.628e6, KB_GB = 1024 * 1024;
  approx(t.cfRequestsM, 2500 * SEC / 1e6, 1e-6);           // platformRps(2500) × sec/month
  approx(t.cfEgressGB, t.cfRequestsM * 1e6 * 8 / KB_GB, 1e-6);  // × avgResponseKB(8)
  assert.ok(t.cfRequestsM > 6000, `cfRequestsM ${t.cfRequestsM}`);
});

// M13-2 — NAT GB derived from node count.
test('M13: NAT GB derived from totalNodes × natGBPerNodeMonth', () => {
  const t = traffic(scn());
  const totalNodes = cost(scn()).totalNodes;
  approx(t.natGB, totalNodes * 20, 1e-9);
});

// M13-3 — cross-AZ transfer is a NEW cost bucket (>0 at default; scales with internal RPS).
test('M13: cross-AZ data transfer is a new bucket, scales with internal RPS', () => {
  const t = traffic(scn());
  assert.ok(t.crossAzGB > 0, 'cross-AZ > 0');
  const t2 = traffic(scn({ tenants: 100000 }));
  assert.ok(t2.crossAzGB > t.crossAzGB * 1.9, 'scales ~linearly with tenants');
});

// M13-4 — inter-region transfer: 0 single-region, >0 only under active-active replication.
test('M13: inter-region transfer is 0 single-region, >0 under active-active', () => {
  assert.equal(traffic(scn({ regions: 1 })).interRegionGB, 0);
  assert.equal(traffic(scn({ regions: 3, crossRegionReplication: false })).interRegionGB, 0);  // sharded doesn't replicate
  assert.ok(traffic(scn({ regions: 3, crossRegionReplication: true })).interRegionGB > 0, 'active-active replicates');
});

// M13-5 — costBreakdown sums to cost().total across postures (incl. the new lines).
test('M13: costBreakdown sums to cost().total (single + sharded + active-active)', () => {
  for (const ov of [{ regions: 1 }, { regions: 3, crossRegionReplication: false }, { regions: 3, crossRegionReplication: true }]) {
    const s = scn(ov);
    const sum = costBreakdown(s).reduce((a, i) => a + i.subtotal, 0);
    approx(sum, cost(s).total, 1e-6);
  }
  // the two new line items exist
  const ids = costBreakdown(scn({ regions: 3, crossRegionReplication: true })).map((i) => i.id);
  assert.ok(ids.includes('crossAz') && ids.includes('interRegion'), 'new data-transfer line items present');
});

// M13-6 — the anchor MOVED to $76,656 (deliberate; regression-locked to the derived value).
test('M13: new anchor $76,656.30 (moved from $72,565.5 — more complete cost)', () => {
  approx(cost(scn()).total, NEW_ANCHOR, 1e-4);
  assert.ok(cost(scn()).total > 72565.5, 'anchor moved up (added traffic + data transfer)');
});

// M13-7 — region scaling stays coherent: single < sharded < active-active.
test('M13: region cost scaling coherent (single < sharded < active-active)', () => {
  const single = cost(scn({ regions: 1 })).total;
  const sharded = cost(scn({ regions: 3, crossRegionReplication: false })).total;
  const aa = cost(scn({ regions: 3, crossRegionReplication: true })).total;
  assert.ok(single < sharded && sharded < aa, `${single} < ${sharded} < ${aa}`);
});

// M13-8 — boundary: 0 tenants => finite traffic, no NaN.
test('M13: boundary tenants=0 => finite traffic + cost, no NaN', () => {
  const t = traffic(scn({ tenants: 0 }));
  for (const k of ['cfRequestsM', 'cfEgressGB', 'crossAzGB', 'natGB', 'interRegionGB'])
    assert.ok(Number.isFinite(t[k]), `${k} finite`);
  assert.ok(Number.isFinite(cost(scn({ tenants: 0 })).total), 'cost finite');
});

// ==================== M14 — Well-Architected scorecard (in-product) ====================
// Grounded in docs/aws-well-architected-review.md (HRI register H1–H5). The scorecard is
// scenario-driven where the scenario controls the dimension (H1 region, H3 db-subnet tier)
// and honest-static where it does not (H2 deviation, H4/H5 always open). It NEVER claims 100%.

const PILLAR_NAMES = ['Operational Excellence', 'Security', 'Reliability', 'Performance Efficiency', 'Cost Optimization', 'Sustainability'];
const hriById = (wa, id) => wa.hris.find(h => h.id === id);
const pillarByName = (wa, name) => wa.pillars.find(p => p.name === name);

test('M14 shape: wellArchitected returns the 6 WA pillars (posture+score+findings) + an HRI list', () => {
  const wa = wellArchitected(scn());
  assert.equal(wa.pillars.length, 6, 'exactly 6 pillars');
  assert.deepEqual(wa.pillars.map(p => p.name), PILLAR_NAMES, 'canonical WA pillar order');
  for (const p of wa.pillars) {
    assert.ok(['strong', 'fair', 'at-risk', 'not-assessable'].includes(p.posture), `${p.name} posture=${p.posture}`);
    assert.ok(Number.isFinite(p.score) && p.score >= 0 && p.score <= 100, `${p.name} score=${p.score}`);
    assert.ok(Array.isArray(p.findings), `${p.name} findings[]`);
  }
  assert.ok(Array.isArray(wa.hris) && wa.hris.length >= 5, 'HRI register H1..H5');
  assert.ok(Number.isFinite(wa.openHriCount), 'openHriCount');
  assert.ok(typeof wa.worstPillar === 'string', 'worstPillar');
});

test('M14 H1 (single-region): fires open at 1 region, mitigated when sharded, cleared active-active', () => {
  assert.equal(hriById(wellArchitected(scn({ regions: 1 })), 'H1').status, 'open');
  assert.equal(hriById(wellArchitected(scn({ regions: 3, crossRegionReplication: false })), 'H1').status, 'mitigated');
  assert.equal(hriById(wellArchitected(scn({ regions: 3, crossRegionReplication: true })), 'H1').status, 'cleared');
  // single-region makes Reliability at-risk
  assert.equal(pillarByName(wellArchitected(scn({ regions: 1 })), 'Reliability').posture, 'at-risk');
});

test('M14 H3 (DB subnet tier): the dbSubnetTier toggle flips the Security HRI open/cleared', () => {
  const off = wellArchitected(scn({ dbSubnetTier: false }));
  const on = wellArchitected(scn({ dbSubnetTier: true }));
  assert.equal(hriById(off, 'H3').status, 'open', 'no DB tier => H3 open');
  assert.equal(hriById(on, 'H3').status, 'cleared', 'DB tier isolated => H3 cleared');
  assert.equal(hriById(off, 'H3').pillar, 'Security');
});

test('M14 honest-static: H2 (deliberate), H4 + H5 (open) are present regardless of scenario', () => {
  // even at the most-remediated scenario, the not-scenario-driven HRIs stay honestly present
  const wa = wellArchitected(scn({ regions: 3, crossRegionReplication: true, dbSubnetTier: true }));
  assert.equal(hriById(wa, 'H2').status, 'deliberate', 'single-account is an ADR deviation, not a fresh open HRI');
  assert.equal(hriById(wa, 'H4').status, 'open', 'no runtime threat-detection stays open (not-in-scope)');
  assert.equal(hriById(wa, 'H5').status, 'open', 'no tested DR/backup-restore stays open (future AM9)');
});

test('M14 remediation: every HRI has an id in H1..H5 + a non-empty remediation string', () => {
  const wa = wellArchitected(scn());
  for (const h of wa.hris) {
    assert.match(h.id, /^H[1-5]$/, `hri id ${h.id}`);
    assert.ok(typeof h.remediation === 'string' && h.remediation.length > 0, `${h.id} remediation`);
    assert.ok(PILLAR_NAMES.includes(h.pillar), `${h.id} pillar ${h.pillar}`);
  }
});

test('M14 burndown: pulling scenario levers reduces the open-HRI count (not to zero — static HRIs remain)', () => {
  const base = wellArchitected(scn());                                                   // regions=1, dbSubnetTier=false
  const remediated = wellArchitected(scn({ regions: 3, crossRegionReplication: true, dbSubnetTier: true }));
  assert.ok(remediated.openHriCount < base.openHriCount, `${remediated.openHriCount} < ${base.openHriCount}`);
  assert.ok(remediated.openHriCount >= 2, 'H4 + H5 remain open (honest: not scenario-driven)');
});

test('M14 honesty: the scorecard is NOT a certificate — never claims 100% Well-Architected', () => {
  const wa = wellArchitected(scn({ regions: 3, crossRegionReplication: true, dbSubnetTier: true }));
  assert.equal(wa.isCertificate, false, 'isCertificate flag is always false (WA is a loop, not a state)');
  // even fully-remediated-by-scenario, at least one pillar is not "strong" (static HRIs / not-assessable)
  assert.ok(wa.pillars.some(p => p.posture !== 'strong'), 'no all-green certificate is possible');
});

test('M14 H5 honesty: active-active clears H1 but Reliability is NOT strong while H5 (no tested DR) is open', () => {
  const wa = wellArchitected(scn({ regions: 3, crossRegionReplication: true }));
  assert.equal(hriById(wa, 'H1').status, 'cleared', 'aa clears the region HRI');
  assert.equal(hriById(wa, 'H5').status, 'open', 'DR restore still untested');
  assert.notEqual(pillarByName(wa, 'Reliability').posture, 'strong', 'pillar stays elevated — honest');
});

test('M14 boundary tenants=0: still 6 pillars, HRIs resolve, no NaN', () => {
  const wa = wellArchitected(scn({ tenants: 0 }));
  assert.equal(wa.pillars.length, 6);
  for (const p of wa.pillars) assert.ok(Number.isFinite(p.score), `${p.name} score finite`);
  assert.ok(Number.isFinite(wa.openHriCount));
});

test('M14 no false all-green: every pillar carries the doc MRIs — Sustainability is not a green empty chip', () => {
  const wa = wellArchitected(scn());
  // regression guard for the M14 review MEDIUM: Sustainability had a false 'strong'/100 chip while the
  // review doc raises a Sustainability MRI. No pillar may render 'strong' with zero findings.
  for (const p of wa.pillars)
    assert.ok(!(p.posture === 'strong' && p.findings.length === 0), `${p.name}: false all-green (strong + 0 findings)`);
  assert.notEqual(pillarByName(wa, 'Sustainability').posture, 'strong', 'Sustainability carries its doc MRI (not a false green)');
  // Cost Optimization now has all 3 doc MRIs (Spot + Savings Plans + gateway endpoints)
  assert.ok(pillarByName(wa, 'Cost Optimization').findings.filter((f) => f.kind === 'mri').length >= 3, 'all 3 Cost MRIs present');
});

// ==================== M15 — Reliability build-out ====================
// Multi-AZ RDS failover time + backup/PITR DR gate + per-AZ NAT become scenario-driven reliability
// inputs; H5 (no tested DR) + the single-NAT MRI clear via real levers. Grounded in
// best-practices § 4 (Multi-AZ + 2 readable standbys → failover < 35s) + the WA review Reliability pillar.

const relBlock = (wa) => wa.reliability;
const hriM15 = (wa, id) => wa.hris.find((h) => h.id === id);
const relFindings = (wa) => wa.pillars.find((p) => p.name === 'Reliability').findings;

test('M15 rdsFailoverSec: failover time modelled by posture (readable<classic<restore<no-recovery)', () => {
  assert.equal(rdsFailoverSec(scn({ rdsMultiAz: true, rdsReadableStandbys: true })), 30, 'Multi-AZ + 2 readable standbys → <35s');
  assert.equal(rdsFailoverSec(scn({ rdsMultiAz: true, rdsReadableStandbys: false })), 90, 'classic Multi-AZ standby → ~90s');
  assert.equal(rdsFailoverSec(scn({ rdsMultiAz: false, rdsBackupPitr: true })), 900, 'single-AZ + PITR → restore ~15min');
  assert.equal(rdsFailoverSec(scn({ rdsMultiAz: false, rdsBackupPitr: false })), Infinity, 'no Multi-AZ, no backup → no tested recovery');
});

test('M15 H5: the backup/PITR + Multi-AZ gate drives the DR HRI open→mitigated→cleared', () => {
  assert.equal(hriM15(wellArchitected(scn({ rdsBackupPitr: false })), 'H5').status, 'open', 'no backup → H5 open');
  assert.equal(hriM15(wellArchitected(scn({ rdsBackupPitr: true, rdsMultiAz: false })), 'H5').status, 'mitigated', 'backup single-AZ → mitigated (restore path, untested failover)');
  assert.equal(hriM15(wellArchitected(scn({ rdsBackupPitr: true, rdsMultiAz: true })), 'H5').status, 'cleared', 'backup + Multi-AZ → cleared');
});

test('M15 per-AZ NAT: the single-NAT reliability MRI clears with the perAzNat lever', () => {
  const single = relFindings(wellArchitected(scn({ perAzNat: false })));
  const perAz = relFindings(wellArchitected(scn({ perAzNat: true })));
  assert.ok(single.some((f) => f.kind === 'mri' && /single NAT/i.test(f.title)), 'single NAT → MRI present');
  assert.ok(!perAz.some((f) => /single NAT/i.test(f.title)), 'per-AZ NAT → single-NAT MRI cleared');
});

test('M15 H1 still reflected: region posture unchanged by the new reliability levers (regression)', () => {
  assert.equal(hriM15(wellArchitected(scn({ regions: 1 })), 'H1').status, 'open');
  assert.equal(hriM15(wellArchitected(scn({ regions: 3, crossRegionReplication: false })), 'H1').status, 'mitigated');
  assert.equal(hriM15(wellArchitected(scn({ regions: 3, crossRegionReplication: true })), 'H1').status, 'cleared');
});

test('M15 reliability metrics block: failoverSec + Multi-AZ/backup/per-AZ-NAT posture surfaced', () => {
  const r = relBlock(wellArchitected(scn({ rdsMultiAz: true, rdsReadableStandbys: true, rdsBackupPitr: true, perAzNat: true })));
  assert.ok(r, 'reliability block present');
  assert.equal(r.failoverSec, 30);
  assert.equal(r.multiAz, true); assert.equal(r.readableStandbys, true);
  assert.equal(r.backupPitr, true); assert.equal(r.perAzNat, true);
  assert.ok(typeof r.regionStatus === 'string', 'regionStatus (H1) surfaced');
});

test('M15 earned green: full remediation drives Reliability to strong — but the board is never all-strong', () => {
  const wa = wellArchitected(scn({ regions: 3, crossRegionReplication: true, rdsBackupPitr: true, rdsMultiAz: true, perAzNat: true }));
  assert.equal(wa.pillars.find((p) => p.name === 'Reliability').posture, 'strong', 'all Reliability findings cleared via real levers → EARNED strong');
  // the scorecard honesty invariant still holds: Security (H4 static) + OpEx (not-assessable) keep it honest
  assert.equal(wa.isCertificate, false);
  assert.ok(wa.pillars.some((p) => p.posture !== 'strong'), 'never an all-strong board');
  assert.equal(hriM15(wa, 'H4').status, 'open', 'H4 (threat-detection) still open — no false 100%');
});

test('M15 default Reliability stays at-risk (H1 + H5 open by default, single NAT)', () => {
  const wa = wellArchitected(scn());
  assert.equal(wa.pillars.find((p) => p.name === 'Reliability').posture, 'at-risk');
  assert.equal(hriM15(wa, 'H5').status, 'open');
  assert.equal(wa.reliability.failoverSec, Infinity, 'default: no tested recovery path');
});

test('M15 anchor + cost untouched: reliability is not priced (failover is a time dim; cost is M16)', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
  for (const ov of [{ rdsMultiAz: true }, { rdsReadableStandbys: true }, { rdsBackupPitr: true }, { perAzNat: true }])
    approx(cost(scn(ov)).total, cost(scn()).total, 1e-9, `${JSON.stringify(ov)} perturbs no cost`);
});

test('M15 boundary tenants=0: failoverSec resolves, 6 pillars, no NaN', () => {
  const wa = wellArchitected(scn({ tenants: 0 }));
  assert.equal(wa.pillars.length, 6);
  const fo = rdsFailoverSec(scn({ tenants: 0 }));
  assert.ok(fo === Infinity || Number.isFinite(fo), 'failoverSec finite or Infinity');
  assert.ok(Number.isFinite(wa.openHriCount));
});

// ==================== M16 — Cost-optimization levers ====================
// Spot on the build plane + Savings-Plan/RI/On-Demand pricing spectrum + gateway VPC endpoints.
// Pricing multipliers are REAL Vantage data (m8g.2xlarge · us-east-1 · Linux · 2026-07), not fabricated.

const costMri = (wa, re) => wa.pillars.find((p) => p.name === 'Cost Optimization').findings.filter((f) => f.kind === 'mri' && re.test(f.title));

test('M16 Vantage provenance: pricing multipliers derive from the documented real $/hr', () => {
  // baseline = 1yr No-Upfront RI $0.2375/hr; the stored ec2Node ($173) is that × 730.
  approx(0.2375 * 730, 173, 0.5, 'ec2Node is the 1yr No-Upfront RI monthly');
  approx(PRICING_MULT['on-demand'], 0.35904 / 0.2375, 1e-4);   // real on-demand / RI
  assert.equal(PRICING_MULT.ri, 1.0);
  approx(PRICING_MULT['compute-sp'], 0.26126 / 0.2375, 1e-4);   // 1yr Compute SP
  approx(PRICING_MULT['sp-3yr'], 0.15592 / 0.2375, 1e-4);       // 3yr Compute SP
  approx(SPOT_FRACTION_OF_ONDEMAND, 0.12452 / 0.35904, 1e-4);   // Spot Avg / on-demand (−65.3%)
  // ordering: on-demand > compute-sp > ri > sp-3yr
  assert.ok(PRICING_MULT['on-demand'] > PRICING_MULT['compute-sp'] && PRICING_MULT['compute-sp'] > PRICING_MULT.ri && PRICING_MULT.ri > PRICING_MULT['sp-3yr']);
});

test('M16 pricingModel scales the SERVING EC2 compute (not EKS fee / RDS)', () => {
  const base = cost(scn());                                   // ri baseline
  const od = cost(scn({ pricingModel: 'on-demand' }));
  const sp3 = cost(scn({ pricingModel: 'sp-3yr' }));
  // compute delta is exactly ec2 compute × (mult − 1); eksCluster + control-plane + global unchanged
  const ec2Base = PRICES.ec2Node * base.totalNodes;
  approx(od.compute - base.compute, ec2Base * (PRICING_MULT['on-demand'] - 1), 1e-6);
  assert.ok(od.total > base.total && sp3.total < base.total, `${sp3.total} < ${base.total} < ${od.total}`);
});

test('M16 buildCost: Spot cuts build-plane compute ~65%; nodes = ⌈concurrent/perNode⌉', () => {
  const noSpot = buildCost(scn({ buildSpotFraction: 0 }));
  const allSpot = buildCost(scn({ buildSpotFraction: 1 }));
  assert.equal(noSpot.nodes, Math.ceil(buildPlane(scn()).concurrentBuilds / buildPlane(scn()).buildsPerNode));
  approx(noSpot.onDemand, noSpot.nodes * PRICES.ec2Node * PRICING_MULT['on-demand'], 1e-6);
  approx(allSpot.total / noSpot.onDemand, SPOT_FRACTION_OF_ONDEMAND, 1e-6, 'full Spot = 34.7% of on-demand');
  approx(allSpot.savings, noSpot.onDemand * (1 - SPOT_FRACTION_OF_ONDEMAND), 1e-6);
});

test('M16 gateway endpoints: reduce the NAT-GB cost bucket by the S3/ECR traffic fraction', () => {
  const base = cost(scn());
  const gw = cost(scn({ gatewayEndpoints: true }));
  const t = traffic(scn());
  approx(base.total - gw.total, PRICES.natGB * t.natGB * GATEWAY_NAT_REDUCTION_FRAC, 1e-6);
  assert.ok(gw.total < base.total, 'gateway endpoint lowers cost');
});

test('M16 Cost MRIs are scenario-driven: each lever clears its MRI (earned green when all 3)', () => {
  assert.ok(costMri(wellArchitected(scn()), /Spot/).length === 1, 'default: Spot MRI present');
  assert.ok(costMri(wellArchitected(scn({ buildSpotFraction: 0.5 })), /Spot/).length === 0, 'Spot lever clears it');
  assert.ok(costMri(wellArchitected(scn({ gatewayEndpoints: true })), /Gateway/).length === 0, 'gateway lever clears it');
  const all = wellArchitected(scn({ buildSpotFraction: 1, pricingModel: 'compute-sp', gatewayEndpoints: true }));
  assert.equal(all.pillars.find((p) => p.name === 'Cost Optimization').posture, 'strong', 'all 3 cleared → Cost strong');
  assert.ok(all.pillars.some((p) => p.posture !== 'strong'), 'board never all-strong (Security H4)');
  assert.equal(all.isCertificate, false);
});

test('M16 honest SP vs RI: pricingModel=ri does NOT clear the Savings-Plan MRI; compute-sp does', () => {
  assert.equal(costMri(wellArchitected(scn({ pricingModel: 'ri' })), /Savings Plan/).length, 1, 'RI ≠ committing to a flexible SP');
  assert.equal(costMri(wellArchitected(scn({ pricingModel: 'compute-sp' })), /Savings Plan/).length, 0, 'compute-SP clears it');
  assert.equal(costMri(wellArchitected(scn({ pricingModel: 'sp-3yr' })), /Savings Plan/).length, 0, '3yr SP clears it');
});

test('M16 anchor preserved at defaults: ri + no-gateway reproduces $76,656 exactly', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
  approx(cost(scn({ pricingModel: 'ri', gatewayEndpoints: false })).total, cost(scn()).total, 1e-9);
});

test('M16 costBreakdown identity holds under every lever (on-demand + gateway + sp-3yr)', () => {
  for (const ov of [{ pricingModel: 'on-demand' }, { gatewayEndpoints: true }, { pricingModel: 'sp-3yr', gatewayEndpoints: true }]) {
    const s = scn(ov);
    const sum = costBreakdown(s).reduce((a, i) => a + i.subtotal, 0);
    approx(sum, cost(s).total, 1e-2, `breakdown sums to total ${JSON.stringify(ov)}`);
  }
});

test('M16 boundary tenants=0: buildCost has 0 nodes, no NaN', () => {
  const b = buildCost(scn({ tenants: 0 }));
  assert.equal(b.nodes, 0);
  assert.ok(Number.isFinite(b.total) && Number.isFinite(b.savings));
  assert.ok(Number.isFinite(cost(scn({ tenants: 0, pricingModel: 'on-demand', gatewayEndpoints: true })).total));
});

// ==================== M17 — Fidelity overlay (cost line items) ====================
const FIDELITY_TIERS_M17 = ['BUILT', 'TARGET', 'CAPSIM', 'SA-inferred'];

test('M17 cost fidelity: every costBreakdown line item carries a fidelity tag (CAPSIM projection)', () => {
  for (const ov of [{}, { regions: 3, crossRegionReplication: true }, { gatewayEndpoints: true }]) {
    const items = costBreakdown(scn(ov));
    assert.ok(items.length > 0);
    for (const it of items) {
      assert.ok(FIDELITY_TIERS_M17.includes(it.fidelity), `${it.id} fidelity=${it.fidelity}`);
      assert.equal(it.fidelity, 'CAPSIM', `${it.id} is a modelled projection`);
    }
  }
});

test('M17 anchor untouched: fidelity is metadata — subtotals + total unchanged, still $76,656', () => {
  approx(cost(scn()).total, 76656.2994, 1e-2);
  // adding a fidelity field must not change any subtotal (sum-to-total still holds)
  const sum = costBreakdown(scn()).reduce((a, i) => a + i.subtotal, 0);
  approx(sum, cost(scn()).total, 1e-2);
});
