// loopzero-capsim.mjs — LoopZero bottleneck simulation engine (pure, no DOM, deterministic).
// Models LoopZero's AWS-first target to find where the architecture bottlenecks at scale.
// Every constant traces to code (file:line) or an AWS-documented default — see notes.
// Consumed by loopzero-bottleneck-simulator.html and tested by loopzero-capsim.test.mjs.

export const DEFAULTS = {
  // --- load ---
  tenants: 50000,
  activeFraction: 0.08,              // % of tenants with a running pod (KEDA scale-to-zero, ≥Pro)
  podsPerTenant: 1,
  platformRpsPerTenant: 0.05,        // dashboard/CLI/status polling per tenant
  agentRpsPerActiveTenant: 2.0,      // memory/promptly hits per active tenant (AI agent traffic)
  ragRpsPerActiveTenant: 0.5,        // rag domain reads per active tenant

  // --- topology ---
  prefixDelegation: false,           // AWS VPC-CNI prefix delegation (raises ENI pod ceiling)
  zones: 3,                          // AZs
  maxPerCell: 500,                   // placement/engine.go:91 — tenants per cell (EKS cluster)
  cellSoftCeiling: 64,               // POLICY input: max EKS clusters the team can operate sanely
  nodeSoftCeiling: 400,              // POLICY input: max app nodes before node-group review
  overheadPerCell: 2,               // system/overhead nodes per EKS cell (aws-cost-calculator default)
  fixedNodes: 10,                   // platform-wide fixed nodes (aws-cost-calculator default)

  // --- VPC CNI subnet IP capacity (M11) — one private-app subnet per AZ ---
  subnetPrefixLen: 20,              // private-app subnet size (/20 = 4091 usable IPs; AWS reserves 5)
  warmIpsPerNode: 16,               // ipamd warm pool per node (WARM_ENI_TARGET ≈ 1 ENI). ESTIMATE.
  customNetworking: false,          // pods draw from a 100.64.0.0/10 secondary CIDR (off the VPC /20)

  node: { vcpu: 8, memGiB: 32, reservePct: 0.10 },   // m8g.2xlarge (Vantage/cost-calc)
  tenantPod: { cpuM: 100, memMi: 128 },              // renderer.go:161-164
  eniMaxPods: 58,                    // AWS eni-max-pods m8g.2xlarge (no prefix delegation)
  eniMaxPodsPrefix: 250,             // with prefix delegation
  kubeletMaxPods: 110,               // kubelet default maxPods

  // --- control-plane request tiers (rps capacity = replicas * muRps) ---
  // muRps = per-replica throughput. ESTIMATE — calibrate with a load test (not a hard fact).
  // cpuM/memMi = per-pod resource REQUESTS (charts) — used for mgmt-cluster node packing (M8).
  bff: { replicas: 6, muRps: 400, cpuM: 50, memMi: 64 },          // loopzero-cloud BFF. requests: charts/loopzero-cloud/values.yaml:64-66 (50m/64Mi; 128Mi is the LIMIT). muRps=ESTIMATE.
  engineApi: { replicas: 8, muRps: 350, cpuM: 100, memMi: 64 },   // loopzero-api Engine API. requests: charts/loopzero-api/values.yaml:181-184 (100m/64Mi). muRps=ESTIMATE.

  // --- mgmt / control-plane cluster (hosts BFF + Engine-API + operators + argocd + gitea + kratos
  // + hydra + redis + minio + traefik + cert-manager + ESO + observability). BFF/Engine replicas
  // PACK onto these nodes (M8). platform baseline = ESTIMATE for the fixed set (labelled, like muRps);
  // fixedNodes below is the HA/headroom FLOOR — the model is floor-dominated at default, coupled at scale.
  mgmt: { platformCpuM: 24000, platformMemMi: 73728, nodeSoftCeiling: 30 },

  // --- build plane cluster (Argo Workflows + BuildKit + cosign) — M12 ---
  // concurrent-build capacity = build nodes × builds/node; demand driven by the deploy rate (Little's law).
  deploysPerActiveTenantPerDay: 2,  // ESTIMATE — active tenants deploy ~2×/day
  buildDurationSec: 240,            // ~4 min deploy (loopzero/CLAUDE.md). ESTIMATE.
  build: { softCeiling: 20, cpuPerBuildM: 2000, memPerBuildMi: 4096 },  // build node ceiling + per-build resources

  // --- per-domain RDS (control-plane service DBs, not tenant DBs) ---
  // max_connections COMPUTED from instanceClassMemGiB via rdsMaxConns (AWS formula). holdSec=ESTIMATE.
  rds: {
    memory:   { instanceClassMemGiB: 32, replicas: 2, holdSec: 0.015 }, // db.m8g.2xlarge, read-heavy pgvector
    rag:      { instanceClassMemGiB: 32, replicas: 2, holdSec: 0.020 }, // db.m8g.2xlarge
    promptly: { instanceClassMemGiB: 8,  replicas: 1, holdSec: 0.008 }, // db.m8g.large, credential broker (per-agent)
  },
  proxyMaxConnPct: 100,                     // RDS Proxy: % of DB max_connections the proxy may use
  regionalOutageAvail: 0.95,                // POLICY/ESTIMATE — availability during a full-region outage (DR posture)
  regions: 1,                               // AWS regions the platform spans (1 = current single-region)
  crossRegionReplication: false,            // active-active (replicated) vs sharded (sticky cells, no failover)
  dbSubnetTier: false,                      // M14/WA-H3: dedicated isolated DB subnet tier (no NAT route). false = current build (HRI open)
  // --- M15/reliability build-out (all default false = current honest reality; enabling clears HRI H5 / single-NAT MRI in the model) ---
  rdsMultiAz: false,                        // Multi-AZ RDS standby (failover). false = not proven/built (WA doc: Multi-AZ is target)
  rdsReadableStandbys: false,               // the "2 readable standbys" variant → failover <35s + read capacity (best-practices § 4)
  rdsBackupPitr: false,                     // automated backups + PITR enabled (DR gate; read-replica prerequisite)
  perAzNat: false,                          // per-AZ NAT (prod) vs single-AZ NAT HA (sandbox). false = single NAT (WA MRI)
  // --- M16/cost-optimization levers (defaults reproduce the $76,656 serving anchor exactly) ---
  pricingModel: 'ri',                       // serving EC2 compute vehicle: on-demand | ri (1yr No-Upfront, baseline) | compute-sp | sp-3yr
  buildSpotFraction: 0,                     // fraction of build-plane nodes on Spot (0..1); builds are interruptible → Spot candidates
  gatewayEndpoints: false,                  // S3/DynamoDB gateway VPC endpoints (keep that traffic off the NAT meter)

  // --- cache ---
  cache: { nodeMiB: 13000, kbPerTenant: 64 },   // ElastiCache r7g.large (~13GiB usable), Valkey

  // --- traffic / data-transfer (M13) — ESTIMATES; calibrate with CloudFront + VPC Flow Logs ---
  avgResponseKB: 8,          // avg CloudFront response size (dashboard/API/polling JSON)
  avgInternalReqKB: 4,       // avg internal request payload (BFF→Engine→cell→RDS hop)
  crossAzFraction: 0.5,      // fraction of internal traffic crossing an AZ boundary
  natGBPerNodeMonth: 20,     // NAT egress per node/month (image pulls, external APIs)

  // --- availability (for chaos SLA estimate) ---
  perInstanceAvail: 0.999,
};

// Deep-ish merge of a scenario override onto DEFAULTS (one level of nesting).
export function scn(override = {}) {
  const base = structuredClone(DEFAULTS);
  for (const [k, v] of Object.entries(override)) {
    if (v && typeof v === 'object' && !Array.isArray(v) && base[k] && typeof base[k] === 'object') {
      base[k] = { ...base[k], ...v };
    } else base[k] = v;
  }
  // Flat aliases so MCP/UI can pass replica counts without nested objects (flat wins over nested).
  // Guard ≥1: a direct caller passing 0/negative is ignored (keeps the default) — MCP/UI already clamp.
  if (base.bffReplicas != null) { if (base.bffReplicas >= 1) base.bff.replicas = base.bffReplicas; delete base.bffReplicas; }
  if (base.engineReplicas != null) { if (base.engineReplicas >= 1) base.engineApi.replicas = base.engineReplicas; delete base.engineReplicas; }
  return base;
}

const ceilDiv = (a, b) => (b > 0 ? Math.ceil(a / b) : 0);
// utilization. No capacity + demand = fully saturated (Infinity); no capacity + no demand = 0.
export const rhoOf = (used, cap) => (cap > 0 ? used / cap : (used > 0 ? Infinity : 0));
// tenants at which used=cap. Guarded: a zero load-term denominator => Infinity (never NaN).
const satAt = (num, den) => (den > 0 ? Math.round(num / den) : Infinity);
// AWS RDS Postgres default max_connections = LEAST(DBInstanceClassMemory/9531392, 5000).
export const rdsMaxConns = (memGiB) => Math.min(Math.floor(memGiB * 1024 ** 3 / 9531392), 5000);

// EKS pod density per node: min of the CPU / memory / ENI / kubelet ceilings.
export function podsPerNode(s) {
  const allocCpuM = s.node.vcpu * 1000 * (1 - s.node.reservePct);
  const allocMemMi = s.node.memGiB * 1024 * (1 - s.node.reservePct);
  const cpu = Math.floor(allocCpuM / s.tenantPod.cpuM);
  const mem = Math.floor(allocMemMi / s.tenantPod.memMi);
  const eni = s.prefixDelegation ? s.eniMaxPodsPrefix : s.eniMaxPods;
  const kubelet = s.kubeletMaxPods;
  const ceilings = { cpu, mem, eni, kubelet };
  let binding = 'cpu', ppn = cpu;
  for (const k of ['mem', 'eni', 'kubelet']) if (ceilings[k] < ppn) { ppn = ceilings[k]; binding = k; }
  return { ...ceilings, ppn, binding };
}

// Mgmt / control-plane cluster node packing (M8). BFF + Engine-API replicas (× per-pod requests)
// pack onto mgmt worker nodes alongside the fixed platform baseline. mgmtNodes = max(HA floor,
// request-packed nodes). Floor-dominated at default (tiny pods absorbed by headroom); coupled at
// scale (large replica counts force new nodes → cost + a mgmt_nodes bottleneck). Answers the
// "raise BFF/Engine pods but no relation to the mgmt cluster?" gap.
export function mgmtNodes(s) {
  const allocCpuM = s.node.vcpu * 1000 * (1 - s.node.reservePct);
  const allocMemMi = s.node.memGiB * 1024 * (1 - s.node.reservePct);
  const cpuM = s.mgmt.platformCpuM + s.bff.replicas * s.bff.cpuM + s.engineApi.replicas * s.engineApi.cpuM;
  const memMi = s.mgmt.platformMemMi + s.bff.replicas * s.bff.memMi + s.engineApi.replicas * s.engineApi.memMi;
  const cpuNodes = ceilDiv(cpuM, allocCpuM);
  const memNodes = ceilDiv(memMi, allocMemMi);
  const packed = Math.max(cpuNodes, memNodes);
  const nodes = Math.max(s.fixedNodes, packed);   // HA/headroom floor
  return { nodes, packed, cpuM, memMi, binding: cpuNodes >= memNodes ? 'cpu' : 'mem', floorBound: packed <= s.fixedNodes };
}

// VPC CNI subnet IP capacity (M11). Each pod consumes ONE IP from the private-app subnet in
// secondary-IP mode; a /20 has 4091 usable IPs. Prefix delegation packs pods into /28 prefixes;
// custom networking moves pods to a 100.64.0.0/10 secondary CIDR (off the VPC /20). One private-app
// subnet per AZ. This is the EKS IP-exhaustion wall — distinct from pod-density-per-node.
export function subnetIps(s) {
  const usablePerSubnet = Math.pow(2, 32 - s.subnetPrefixLen) - 5;   // AWS reserves 5 per subnet
  const appSubnets = s.zones;                                        // one private-app subnet per AZ
  const capacity = usablePerSubnet * appSubnets;
  const { ppn } = podsPerNode(s);
  const activePods = s.tenants * s.activeFraction * s.podsPerTenant;
  const appNodes = ceilDiv(activePods, ppn);
  // The pod-IP warm pool lives on the /20 in secondary/prefix mode; under custom networking pods AND
  // their warm pool move to the 100.64/10 secondary CIDR, leaving only node primary-ENI IPs (≈1/node).
  const warm = appNodes * (s.customNetworking ? 1 : s.warmIpsPerNode);
  const mode = s.customNetworking ? 'custom-networking' : s.prefixDelegation ? 'prefix' : 'secondary';
  const podIps = s.customNetworking ? 0                              // pods on the 100.64/10 CIDR
    : s.prefixDelegation ? Math.ceil(activePods / 16) * 16           // /28 prefix granularity
    : activePods;                                                    // secondary: 1 IP per pod
  const used = podIps + warm;
  // marginal IPs per tenant (drives saturatesAtTenants): pod IPs + the node's /20 IP share.
  const podPerTenant = s.activeFraction * s.podsPerTenant;
  const perTenantIp = (s.customNetworking ? 0 : podPerTenant) + (podPerTenant / Math.max(1, ppn)) * (s.customNetworking ? 1 : s.warmIpsPerNode);
  return { used, capacity, usablePerSubnet, appSubnets, podIps, warm, perTenantIp, mode };
}

// Build plane capacity (M12). The build cluster (Argo Workflows + BuildKit) runs one BuildKit pod per
// build; a build is CPU/mem-heavy. Concurrent-build demand follows Little's law (L = λ·W): arrival
// rate (deploys/sec from active tenants) × build duration. This is a separate cluster from mgmt/workload.
export function buildPlane(s) {
  const allocCpuM = s.node.vcpu * 1000 * (1 - s.node.reservePct);
  const allocMemMi = s.node.memGiB * 1024 * (1 - s.node.reservePct);
  const buildsPerNode = Math.min(Math.floor(allocCpuM / s.build.cpuPerBuildM), Math.floor(allocMemMi / s.build.memPerBuildMi));
  const buildSoftCeiling = s.build.softCeiling;
  const capacity = buildSoftCeiling * buildsPerNode;
  const activeTenants = s.tenants * s.activeFraction;
  const concurrentBuilds = activeTenants * s.deploysPerActiveTenantPerDay / 86400 * s.buildDurationSec;
  const perTenantBuild = s.activeFraction * s.deploysPerActiveTenantPerDay / 86400 * s.buildDurationSec;
  return { concurrentBuilds, capacity, buildsPerNode, buildSoftCeiling, perTenantBuild };
}

// Traffic + data-transfer volumes (M13). Derives the demand-driven edge/network costs (CloudFront
// requests/egress, NAT GB, cross-AZ + inter-region transfer) from the scenario's RPS + node count,
// replacing the fixed COST_QTY estimates. Rates are ESTIMATES (labelled in DEFAULTS).
const SEC_PER_MONTH = 2.628e6;   // 30.42 days
const KB_PER_GB = 1024 * 1024;
export function traffic(s) {
  const platformRps = s.tenants * s.platformRpsPerTenant;                    // CloudFront-fronted (BFF)
  const agentRps = s.tenants * s.activeFraction * s.agentRpsPerActiveTenant;
  const ragRps = s.tenants * s.activeFraction * s.ragRpsPerActiveTenant;
  const internalRps = platformRps + agentRps + ragRps;                       // internal request path
  const cfRequestsM = platformRps * SEC_PER_MONTH / 1e6;                     // millions/month
  const cfEgressGB = cfRequestsM * 1e6 * s.avgResponseKB / KB_PER_GB;
  const crossAzGB = internalRps * SEC_PER_MONTH * s.avgInternalReqKB * s.crossAzFraction / KB_PER_GB;
  const { ppn } = podsPerNode(s);
  const totalNodes = ceilDiv(s.tenants * s.activeFraction * s.podsPerTenant, ppn)
    + ceilDiv(s.tenants, s.maxPerCell) * s.overheadPerCell + mgmtNodes(s).nodes;
  const natGB = totalNodes * s.natGBPerNodeMonth;
  const regions = Math.max(1, s.regions || 1);
  // active-active replicates the internal write stream to every other region
  const interRegionGB = s.crossRegionReplication
    ? internalRps * SEC_PER_MONTH * s.avgInternalReqKB / KB_PER_GB * (regions - 1) : 0;
  return { platformRps, agentRps, ragRps, internalRps, cfRequestsM, cfEgressGB, crossAzGB, natGB, interRegionGB };
}

// Build the component list. survivorFactor<1 models chaos on zone-distributed capacity
// (an AZ down). cellFactor<1 models a full-region loss taking the EKS clusters too — an AZ
// loss does NOT reduce the cluster COUNT you need (clusters span AZs), but a region loss does.
function buildComponents(s, survivorFactor = 1, cellFactor = 1, buildFactor = 1) {
  const activePods = s.tenants * s.activeFraction * s.podsPerTenant;
  const { ppn } = podsPerNode(s);
  const platformRps = s.tenants * s.platformRpsPerTenant;
  const agentRps = s.tenants * s.activeFraction * s.agentRpsPerActiveTenant;
  const ragRps = s.tenants * s.activeFraction * s.ragRpsPerActiveTenant;
  const zf = survivorFactor;
  const ni = subnetIps(s);   // M11: private-app subnet IP capacity
  const bp = buildPlane(s);  // M12: build cluster concurrency

  // reads spread over (1 primary + replicas); RDS Proxy caps at proxyMaxConnPct of DB max_connections.
  const rdsReadCap = (d) => rdsMaxConns(d.instanceClassMemGiB) * (s.proxyMaxConnPct / 100) * (1 + d.replicas);

  const C = [
    // Layer A — packing
    { id: 'cell_count', label: 'Cell / EKS cluster count', unit: 'clusters',
      used: ceilDiv(s.tenants, s.maxPerCell), capacity: Math.round(s.cellSoftCeiling * cellFactor),
      saturatesAtTenants: s.cellSoftCeiling * s.maxPerCell,
      note: 'maxPerCell=500 (engine.go:91) forces one EKS cluster per 500 tenants.' },
    { id: 'app_nodes', label: 'App worker nodes', unit: 'nodes',
      used: ceilDiv(activePods, ppn), capacity: Math.round(s.nodeSoftCeiling * zf),
      saturatesAtTenants: satAt((s.nodeSoftCeiling * zf) * ppn, s.activeFraction * s.podsPerTenant),
      note: `pod density = ${ppn}/node (${podsPerNode(s).binding}-bound).` },
    { id: 'subnet_ips', label: 'Subnet IPs (private-app)', unit: 'IPs',
      used: ni.used, capacity: Math.round(ni.capacity * zf),
      saturatesAtTenants: satAt(ni.capacity * zf, ni.perTenantIp),
      note: `${ni.mode} · ${ni.appSubnets}× /${s.subnetPrefixLen} subnet (${ni.usablePerSubnet} usable each).` },
    { id: 'build_concurrency', label: 'Build plane concurrency', unit: 'builds',
      used: bp.concurrentBuilds, capacity: Math.round(bp.capacity * zf * buildFactor),
      saturatesAtTenants: satAt(bp.capacity * zf, bp.perTenantBuild),   // design saturation (not the transient killBuild snapshot)
      note: `${bp.buildsPerNode} builds/node × ${bp.buildSoftCeiling} nodes; deploy-rate driven (Little's law).` },
    { id: 'mgmt_nodes', label: 'Mgmt / control-plane nodes', unit: 'nodes',
      used: mgmtNodes(s).nodes, capacity: Math.round(s.mgmt.nodeSoftCeiling * zf),
      saturatesAtTenants: Infinity,   // replica/platform-driven, not tenant-driven
      note: `packs BFF(${s.bff.replicas}) + Engine(${s.engineApi.replicas}) + platform baseline; HA floor ${s.fixedNodes}; ${mgmtNodes(s).binding}-bound.` },
    // Layer B — control-plane request queueing
    { id: 'bff', label: 'BFF request tier', unit: 'rps',
      used: platformRps, capacity: Math.round(s.bff.replicas * s.bff.muRps * zf),
      saturatesAtTenants: satAt(s.bff.replicas * s.bff.muRps * zf, s.platformRpsPerTenant),
      note: 'single front door for dashboard/CLI/status.' },
    { id: 'engine_api', label: 'Engine API tier', unit: 'rps',
      used: platformRps, capacity: Math.round(s.engineApi.replicas * s.engineApi.muRps * zf),
      saturatesAtTenants: satAt(s.engineApi.replicas * s.engineApi.muRps * zf, s.platformRpsPerTenant),
      note: 'internal engine API behind the BFF proxy.' },
    { id: 'memory_rds', label: 'RDS conns — memory domain', unit: 'conns',
      used: agentRps * s.rds.memory.holdSec, capacity: Math.round(rdsReadCap(s.rds.memory) * zf),
      saturatesAtTenants: satAt(rdsReadCap(s.rds.memory) * zf, s.activeFraction * s.agentRpsPerActiveTenant * s.rds.memory.holdSec),
      note: 'read-heavy pgvector; reads spread over 2 replicas.' },
    { id: 'rag_rds', label: 'RDS conns — rag domain', unit: 'conns',
      used: ragRps * s.rds.rag.holdSec, capacity: Math.round(rdsReadCap(s.rds.rag) * zf),
      saturatesAtTenants: satAt(rdsReadCap(s.rds.rag) * zf, s.activeFraction * s.ragRpsPerActiveTenant * s.rds.rag.holdSec),
      note: 'rag domain reads.' },
    { id: 'promptly_rds', label: 'RDS conns — promptly (broker)', unit: 'conns',
      used: agentRps * s.rds.promptly.holdSec, capacity: Math.round(rdsReadCap(s.rds.promptly) * zf),
      saturatesAtTenants: satAt(rdsReadCap(s.rds.promptly) * zf, s.activeFraction * s.agentRpsPerActiveTenant * s.rds.promptly.holdSec),
      note: 'credential broker (Model B); hit per active-agent request; db.m8g.large=901 conns.' },
    { id: 'cache_mem', label: 'ElastiCache memory', unit: 'MiB',
      used: s.tenants * s.cache.kbPerTenant / 1024, capacity: Math.round(s.cache.nodeMiB * zf),
      saturatesAtTenants: Math.round(s.cache.nodeMiB * zf * 1024 / s.cache.kbPerTenant),
      note: 'Valkey working set for hot tenant state.' },
  ];
  for (const c of C) c.rho = rhoOf(c.used, c.capacity);
  const rank = r => (r === Infinity ? Number.MAX_VALUE : r);   // ∞-safe, NaN-free ordering on ties
  C.sort((a, b) => rank(b.rho) - rank(a.rho));
  return { components: C, activePods, ppn, platformRps, agentRps };
}

// Series availability of the request path, with per-tier redundancy.
function slaEstimate(s, survivorFactor = 1) {
  const path = [
    { name: 'cloudfront', replicas: 1, base: 0.99995 },
    { name: 'alb', replicas: 1, base: 0.9999 },
    { name: 'bff', replicas: s.bff.replicas },
    { name: 'engine_api', replicas: s.engineApi.replicas },
    { name: 'memory_rds', replicas: 1 + s.rds.memory.replicas },
  ];
  let sla = 1;
  for (const t of path) {
    const rEff = Math.max(1, Math.floor((t.replicas ?? 1) * survivorFactor));
    const a = t.base ?? (1 - Math.pow(1 - s.perInstanceAvail, rEff));  // redundancy raises availability
    sla *= a;
  }
  return Math.round(sla * 1e6) / 1e4;   // percent, 4 decimals
}

export function simulate(s = DEFAULTS) {
  const { components, activePods, ppn, platformRps, agentRps } = buildComponents(s, 1);
  return {
    cells: ceilDiv(s.tenants, s.maxPerCell),
    activePods, ppn,
    appNodes: ceilDiv(activePods, ppn),
    platformRps, agentRps,
    components,
    bottleneck: components[0],
    sla: slaEstimate(s, 1),
    dbSubnetTier: s.dbSubnetTier,   // M14/WA-H3: topology hint (isolated DB tier vs SA-inferred)
  };
}

// Chaos. killAZ removes 1/zones of zone-distributed capacity. killRegion depends on the region
// posture (regions + crossRegionReplication):
//   single (regions=1)        — total outage until DR; SLA = DR floor.
//   sharded (regions≥2, no repl) — survivors run at base ρ; lose 1/regions of tenants; SLA blended.
//   active-active (regions≥2, repl) — 0 tenants lost; survivors absorb failover (ρ ×= r/(r-1)); no DR floor.
export function chaos(s = DEFAULTS, opts = {}) {
  const drFloor = Math.round(Math.min(1, Math.max(0, s.regionalOutageAvail)) * 1e6) / 1e4;
  let survivorFactor = 1, cellFactor = 1, buildFactor = 1, capLossPct = 0, sla, cellTenants = 0, deploysBlocked = false;
  if (opts.killRegion) {
    if (s.regions <= 1) {                          // single-region: total loss
      survivorFactor = 0; cellFactor = 0; capLossPct = 1; sla = drFloor;
    } else if (s.crossRegionReplication) {         // active-active: survivors absorb the dead region
      survivorFactor = (s.regions - 1) / s.regions; cellFactor = survivorFactor;
      capLossPct = 0; sla = slaEstimate(s, survivorFactor);
    } else {                                        // sharded: survivors untouched, lose 1/regions of tenants
      survivorFactor = 1; cellFactor = 1; capLossPct = 1 / s.regions;
      // EXPECTED PER-TENANT availability during the outage (not a nominal SLA): the surviving
      // (1−1/regions) of tenants keep full availability; the killed region's 1/regions sees the DR floor.
      const full = slaEstimate(s, 1);
      sla = Math.round(((1 - 1 / s.regions) * full + (1 / s.regions) * drFloor) * 1e4) / 1e4;
    }
  } else if (opts.killAZ) {
    survivorFactor = (s.zones - 1) / s.zones; capLossPct = 1 - survivorFactor;
    sla = slaEstimate(s, survivorFactor);
  } else if (opts.killCell) {
    // Intra-cell blast radius: one cell (EKS cluster) fails. Sticky placement (ADR-0008/0021)
    // means survivors are UNTOUCHED (no failover load shift) — the blast is CONTAINED, not
    // redistributed. This is the cell architecture's whole justification.
    const cells = ceilDiv(s.tenants, s.maxPerCell);
    cellTenants = s.tenants > 0 ? Math.min(s.maxPerCell, Math.ceil(s.tenants / cells)) : 0;
    capLossPct = s.tenants > 0 ? cellTenants / s.tenants : 0;
    survivorFactor = 1; cellFactor = 1;   // isolation — surviving cells run at base ρ
    const full = slaEstimate(s, 1);
    sla = Math.round(((1 - capLossPct) * full + capLossPct * drFloor) * 1e4) / 1e4;  // blended per-tenant
  } else if (opts.killBuild) {
    // Plane isolation: the build cluster fails → new deploys are blocked, but the SERVING path
    // (request tiers + cells + RDS) is untouched. The opposite of a serving outage.
    buildFactor = 0; deploysBlocked = true;
    survivorFactor = 1; cellFactor = 1; capLossPct = 0;
    sla = slaEstimate(s, 1);   // serving unaffected
  } else {
    sla = slaEstimate(s, 1);
  }
  const { components, activePods, ppn, platformRps, agentRps } = buildComponents(s, survivorFactor, cellFactor, buildFactor);
  return {
    cells: ceilDiv(s.tenants, s.maxPerCell),
    activePods, ppn,
    appNodes: ceilDiv(activePods, ppn),
    platformRps, agentRps,
    components,
    bottleneck: components[0],
    capLossPct,
    cellTenants,
    deploysBlocked,
    sla,
    dbSubnetTier: s.dbSubnetTier,   // M14/WA-H3: topology hint (isolated DB tier vs SA-inferred)
  };
}

// Per-component remediation hints (design judgment — tunable, not physics).
const REVIEW_REC = {
  cell_count:  'raise maxPerCell or accept the EKS cluster count',
  app_nodes:   'enable prefix delegation or use a larger node group',
  bff:         'add BFF replicas or cache/deduplicate status polling',
  engine_api:  'add Engine API replicas or shed control-plane load',
  memory_rds:  'add read replicas or a larger instance class',
  rag_rds:     'add read replicas or a larger instance class',
  promptly_rds:'larger instance class for the credential broker (db.m8g.large=901 conns)',
  cache_mem:   'larger ElastiCache node or shard the working set',
};

// AI Design Review: rubric-based score (0-100) + verdict + findings, GROUNDED on the
// deterministic engine (simulate + chaos). Every finding's number comes from the engine —
// the LLM client narrates on top of these facts, it does not invent metrics.
export function review(s = DEFAULTS) {
  const base = simulate(s);
  const az = chaos(s, { killAZ: true });
  const region = chaos(s, { killRegion: true });
  const azById = (id) => az.components.find((c) => c.id === id);
  const f2 = (r) => (Number.isFinite(r) ? r.toFixed(2) : '∞');

  let score = 100;
  const findings = [];
  for (const c of base.components) {
    if (c.rho >= 1) {
      score -= 20;
      findings.push({ severity: 'critical', component: c.id, basis: 'base', rho: c.rho,
        message: `${c.label} saturated (ρ=${f2(c.rho)}) — bottleneck at this load`,
        recommendation: REVIEW_REC[c.id] || 'scale this tier' });
    } else if (c.rho >= 0.8) {
      score -= 8;
      findings.push({ severity: 'high', component: c.id, basis: 'base', rho: c.rho,
        message: `${c.label} near saturation (ρ=${f2(c.rho)})`,
        recommendation: REVIEW_REC[c.id] || 'add headroom' });
    }
    const a = azById(c.id);
    if (c.rho < 1 && a && a.rho >= 1) {
      score -= 10;
      findings.push({ severity: 'high', component: c.id, basis: 'az-loss', rho: a.rho,
        message: `${c.label} saturates if an AZ drops (ρ=${f2(a.rho)} under AZ loss)`,
        recommendation: 'spread replicas across ≥2 AZs or add headroom' });
    }
  }
  // Region posture (replaces the flat single-region penalty).
  if (s.regions <= 1) {
    score -= 10;
    findings.push({ severity: 'medium', component: 'single_region', basis: 'region', sla: region.sla,
      message: `single-region: SLA falls to ${region.sla}% during a full-region outage`,
      recommendation: 'multi-region: sharded cuts blast radius; active-active adds failover' });
  } else if (!s.crossRegionReplication) {
    score -= 5;
    findings.push({ severity: 'medium', component: 'region_sharded', basis: 'region', capLossPct: region.capLossPct,
      message: `multi-region sharded (${s.regions} regions): a region loss drops ${Math.round(region.capLossPct * 100)}% of tenants — no cross-region failover`,
      recommendation: 'enable cross-region replication (active-active) for full failover' });
  } else {
    // active-active: survivors must absorb the failover load. Surface EVERY region-fragile tier,
    // but CAP the aggregate penalty strictly below the single-region flat −10, so a 0%-blast-radius
    // posture can never score worse than a total-outage one (resilience-ranking invariant).
    let regionFragile = 0;
    for (const c of base.components) {
      const rc = region.components.find((x) => x.id === c.id);
      if (c.rho < 1 && rc && rc.rho >= 1) {
        regionFragile += 5;
        findings.push({ severity: 'high', component: c.id, basis: 'region-loss', rho: rc.rho,
          message: `${c.label} saturates absorbing a region's failover load (ρ=${f2(rc.rho)})`,
          recommendation: 'add headroom so survivors handle N→N-1 region failover' });
      }
    }
    score -= Math.min(8, regionFragile);   // capped < single-region's 10 (invariant: aa ≥ single)
  }
  score = Math.max(0, Math.min(100, score));
  const verdict = score >= 85 ? 'SOUND' : score >= 60 ? 'NEEDS_REVISION' : score >= 40 ? 'AT_RISK' : 'CRITICAL';
  return {
    score, verdict, findings,
    metrics: {
      baseBottleneck: { id: base.bottleneck.id, rho: base.bottleneck.rho },
      azBottleneck: { id: az.bottleneck.id, rho: az.bottleneck.rho },
      regionSla: region.sla, azCapLossPct: az.capLossPct, regionCapLossPct: region.capLossPct,
      costPerMonth: Math.round(cost(s).total),
    },
  };
}

// --- M14: Well-Architected scorecard (in-product). Grounded in docs/aws-well-architected-review.md.
// Scenario-driven where the scenario controls the dimension (H1 region posture, H3 db-subnet tier);
// honest-static where it does not (H2 ADR deviation; H4/H5 always open). NEVER a "100% WA" certificate —
// the doc itself says claiming 100% would violate Operational Excellence ("evolve").
const WA_PILLARS = ['Operational Excellence', 'Security', 'Reliability', 'Performance Efficiency', 'Cost Optimization', 'Sustainability'];

// M15: RDS failover time (seconds) by reliability posture. Grounded in best-practices § 4:
// Multi-AZ + 2 readable standbys → failover < 35s; classic Multi-AZ standby → 60–120s; single-AZ has
// no automatic failover (restore from PITR, RTO minutes); no backups → no tested recovery path (Infinity).
export function rdsFailoverSec(s = DEFAULTS) {
  if (s.rdsMultiAz) return s.rdsReadableStandbys ? 30 : 90;   // <35s (readable standbys) vs classic Multi-AZ
  return s.rdsBackupPitr ? 900 : Infinity;                    // restore-from-PITR (~15min) vs no recovery path
}

export function wellArchitected(s = DEFAULTS) {
  // ---- HRI register (H1..H5). Status derived from the scenario where the scenario controls it. ----
  const h1 = s.regions <= 1 ? 'open' : (s.crossRegionReplication ? 'cleared' : 'mitigated');
  const h3 = s.dbSubnetTier ? 'cleared' : 'open';
  // M15: H5 (tested DR) is scenario-driven — backups+PITR give a restore path; +Multi-AZ gives failover.
  const h5 = (s.rdsBackupPitr && s.rdsMultiAz) ? 'cleared' : (s.rdsBackupPitr ? 'mitigated' : 'open');
  const failoverSec = rdsFailoverSec(s);
  const hris = [
    { id: 'H1', pillar: 'Reliability', title: 'Single-region — 100% blast on region loss', status: h1, severity: 'high', scenarioDriven: true,
      remediation: h1 === 'open' ? 'multi-region: sharded cuts blast radius to 1/N; active-active adds cross-region failover (M6 memo)'
        : h1 === 'mitigated' ? 'sharded reduces blast to 1/N; enable cross-region replication (active-active) for full failover'
        : 'active-active: 0% region blast — keep survivors sized for N→N-1 failover' },
    { id: 'H2', pillar: 'Security', title: 'Single AWS account — no account-level blast/quota isolation', status: 'deliberate', severity: 'high', scenarioDriven: false,
      remediation: 'ADR D4 deliberate trade-off (simplicity); re-evaluate multi-account at the next scale/compliance milestone' },
    { id: 'H3', pillar: 'Security', title: 'No dedicated DB subnet tier', status: h3, severity: 'high', scenarioDriven: true,
      remediation: h3 === 'open' ? 'add an isolated database_subnets tier (no NAT route) to the VPC module before AM4 RDS'
        : 'DB tier network-isolated (no NAT route) — HRI closed' },
    { id: 'H4', pillar: 'Security', title: 'No runtime threat-detection (GuardDuty/Security Hub/Config)', status: 'open', severity: 'high', scenarioDriven: false,
      remediation: 'enable Security Hub FSBP + GuardDuty + Config at account bootstrap (AM2 extension)' },
    { id: 'H5', pillar: 'Reliability', title: 'No tested AWS DR/backup-restore', status: h5, severity: 'high', scenarioDriven: true,
      remediation: h5 === 'open' ? 'enable RDS automated backups + PITR; add Multi-AZ for failover; test a restore + run a game-day'
        : h5 === 'mitigated' ? `restore-from-PITR path exists (~${Math.round(failoverSec / 60)}min RTO); add Multi-AZ for fast failover (<35s) + test it`
        : `Multi-AZ failover (${failoverSec}s) + PITR restore path — DR posture modelled; validate with a real-AWS game-day` },
  ];
  // 'mitigated' (sharded) still counts as an open (partial) HRI for burn-down; 'cleared'/'deliberate' do not.
  const isOpenish = (st) => st === 'open' || st === 'mitigated';
  const openHriCount = hris.filter((h) => isOpenish(h.status)).length;

  // ---- Static (non-scenario) MRIs / not-assessable per pillar, verbatim from the review doc. ----
  const mri = (pillar, title, remediation) => ({ pillar, kind: 'mri', title, remediation });
  const na = (pillar, title) => ({ pillar, kind: 'not-assessable', title });
  const STATIC = {
    'Operational Excellence': [
      na('Operational Excellence', 'AWS deploy/rollback/DR runbooks unproven (no real-AWS apply)'),
      mri('Operational Excellence', 'No observability stack mapped to the AWS target', 'map Prometheus/Loki/Tempo/OTel onto EKS vs CloudWatch/AMP/AMG')],
    'Security': [   // H2/H3/H4 come from the HRI register; these are the doc's Security MRIs
      mri('Security', 'EKS NetworkPolicy not default-on', 'enable VPC-CNI NetworkPolicy at cluster creation; port tenant deny-all policies'),
      mri('Security', 'Security Groups for Pods (pod→RDS) not specified', 'specify SGP for pod→RDS on the AWS target')],
    'Reliability': s.perAzNat ? [] : [   // M15: single-NAT MRI is scenario-driven — cleared by the per-AZ NAT lever
      mri('Reliability', 'Sandbox uses a single NAT (single-AZ HA only)', 'exercise the per-AZ NAT prod path, not just declare it')],
    'Performance Efficiency': [
      mri('Performance Efficiency', 'Read-replica read/write split not modelled as routing', 'route read-heavy domains to the reader endpoint'),
      mri('Performance Efficiency', 'muRps/holdSec are ESTIMATES', 'calibrate with a load test + RDS Performance Insights')],
    'Cost Optimization': [   // M16: each Cost MRI is scenario-driven — cleared by its lever
      ...(s.buildSpotFraction > 0 ? [] : [mri('Cost Optimization', 'No Spot on the build plane', 'shift interruptible build nodes to Spot (−65% on m8g.2xlarge)')]),
      ...((s.pricingModel === 'compute-sp' || s.pricingModel === 'sp-3yr') ? [] : [mri('Cost Optimization', 'Savings Plans uncommitted', 'commit serving compute to a flexible Compute Savings Plan (RI is instance-locked; SP is the doc\'s pick)')]),
      ...(s.gatewayEndpoints ? [] : [mri('Cost Optimization', 'Gateway VPC endpoints (S3/DynamoDB) not in the network design', 'add an S3 gateway endpoint to the VPC (keeps bundle/image traffic off the NAT meter)')])],
    'Sustainability': [   // the doc's Sustainability MRI — no pillar is a false all-green
      mri('Sustainability', 'No explicit sustainability measurement in the ops loop', 'add the AWS Customer Carbon Footprint Tool to the ops loop')],
  };

  // ---- 6 pillars: posture from findings (open HRI => at-risk). score is DESIGN-TIME alignment, NOT a certificate. ----
  const pillars = WA_PILLARS.map((name) => {
    const openHris = hris.filter((h) => h.pillar === name && isOpenish(h.status))
      .map((h) => ({ pillar: name, kind: 'hri', id: h.id, title: h.title, status: h.status, remediation: h.remediation }));
    const closedHris = hris.filter((h) => h.pillar === name && !isOpenish(h.status))
      .map((h) => ({ pillar: name, kind: h.status === 'deliberate' ? 'deviation' : 'cleared', id: h.id, title: h.title, status: h.status, remediation: h.remediation }));
    const statics = STATIC[name] || [];
    const findings = [...openHris, ...statics, ...closedHris];
    const notAssess = statics.some((f) => f.kind === 'not-assessable');
    const mriCount = statics.filter((f) => f.kind === 'mri').length;
    const deviation = closedHris.some((f) => f.kind === 'deviation');
    let posture;
    if (openHris.length > 0) posture = 'at-risk';
    else if (notAssess) posture = 'not-assessable';
    else if (mriCount > 0 || deviation) posture = 'fair';
    else posture = 'strong';
    let score = 100 - 25 * openHris.length - 6 * mriCount - (notAssess ? 15 : 0);
    score = Math.max(0, Math.min(100, score));
    return { id: name.toLowerCase().replace(/[^a-z]+/g, '-'), name, posture, score, findings };
  });
  const order = { 'at-risk': 0, 'not-assessable': 1, fair: 2, strong: 3 };
  const worst = pillars.slice().sort((a, b) => order[a.posture] - order[b.posture])[0];
  // M15: reliability metrics block — failover time + Multi-AZ/backup/per-AZ-NAT posture + region status (H1).
  const reliability = {
    failoverSec, multiAz: s.rdsMultiAz, readableStandbys: s.rdsReadableStandbys,
    backupPitr: s.rdsBackupPitr, perAzNat: s.perAzNat, regionStatus: h1,
  };
  return { pillars, hris, openHriCount, worstPillar: worst.name, isCertificate: false, reliability };
}

// --- Cost model (M4) — unit prices sourced from aws-cost-calculator.html ITEMS (Vantage RI).
// A price-consistency test asserts these equal the calculator's, so the two tools never diverge.
export const PRICES = {
  ec2Node: 173, eksCluster: 73,                              // compute
  rdsMem: 160, rdsRag: 160, rdsLens: 80, rdsProm: 40, rdsTrust: 40, rdsProxy: 50, cacheNode: 128,
  rdsStoreGB: 0.115, s3GB: 0.023, ecrGB: 0.10, cfEgressGB: 0.085, cfReqM: 0.75, natGB: 0.045,
  albMonth: 60, natGW: 33, waf: 1300, r53Zone: 0.90, kmsKey: 1.10, smSecret: 0.45,
  dtCrossAzGB: 0.01, dtInterRegionGB: 0.02,   // M13: cross-AZ + inter-region data transfer ($/GB)
};
// Fixed quantities (calculator ITEMS defaults). Exported so a keyed consistency test can assert
// each equals the calculator's per-item qtyDef (not just membership).
export const COST_QTY = {
  rdsMemN: 4, rdsRagN: 4, rdsLensN: 4, rdsPromN: 3, rdsTrustN: 3, rdsProxyN: 6, cacheN: 3,
  rdsStoreGB: 4000, s3GB: 15000, ecrGB: 60000, cfEgressGB: 50000, cfReqM: 2000, natGBn: 5000,
  albN: 2, natN: 3, wafN: 1, r53N: 8, kmsN: 8, smN: 30,
};

// M16 cost-optimization levers — REAL Vantage data (m8g.2xlarge · us-east-1 · Linux · 2026-07),
// NOT the doc's "up-to" ceilings. Multipliers are vs the current baseline (1yr No-Upfront RI = ec2Node/mo).
// on-demand $0.35904/hr · 1yr-RI $0.2375/hr (baseline) · 1yr Compute-SP $0.26126/hr · 3yr Compute-SP $0.15592/hr · Spot Avg $0.12452/hr.
export const PRICING_MULT = {
  'on-demand': 0.35904 / 0.2375,   // ×1.51175 — the cost of NOT committing
  ri: 1.0,                         // baseline (1yr No-Upfront RI); PRICES.ec2Node already this
  'compute-sp': 0.26126 / 0.2375,  // ×1.10004 — +10% vs RI but flexible (family/size/region) — the doc's pick
  'sp-3yr': 0.15592 / 0.2375,      // ×0.65651 — 3yr Compute SP, deepest commit (−34% vs baseline)
};
export const SPOT_FRACTION_OF_ONDEMAND = 0.12452 / 0.35904;   // 0.34682 — Spot Avg / on-demand (−65.3%; interrupt 15-20%)
export const GATEWAY_NAT_REDUCTION_FRAC = 0.35;               // S3/ECR image+bundle traffic kept off the NAT meter (ESTIMATE — calibrate w/ VPC Flow Logs)

// M16: build-plane monthly compute cost (the burst/interruptible cluster M12 modelled but left unpriced).
// SEPARATE from the serving anchor — build is a distinct cost domain. Build nodes run On-Demand baseline;
// buildSpotFraction shifts them to Spot (−65%). buildNodes provisioned to demand (⌈concurrent/perNode⌉).
export function buildCost(s = DEFAULTS) {
  const bp = buildPlane(s);
  const nodes = bp.buildsPerNode > 0 ? Math.ceil(bp.concurrentBuilds / bp.buildsPerNode) : 0;
  const onDemand = nodes * PRICES.ec2Node * PRICING_MULT['on-demand'];
  const f = Math.max(0, Math.min(1, s.buildSpotFraction || 0));
  const spotBlend = (1 - f) + f * SPOT_FRACTION_OF_ONDEMAND;
  const total = onDemand * spotBlend;
  return { nodes, onDemand, total, savings: onDemand - total, spotFraction: f };
}

// Monthly cost ($) of a scenario, priced with PRICES × the engine's AUTHORITATIVE capacity
// (cells = ceil(tenants/maxPerCell) — NOT the calculator's manual cells input). Region scaling:
// control-plane duplicates per region (sharded + active-active); compute replicates only in
// active-active (sharded splits tenants → same total compute).
export function cost(s = DEFAULTS) {
  const P = PRICES, Q = COST_QTY;
  const cells = ceilDiv(s.tenants, s.maxPerCell);
  const activePods = s.tenants * s.activeFraction * s.podsPerTenant;
  const { ppn } = podsPerNode(s);
  const appNodes = ceilDiv(activePods, ppn);
  const totalNodes = appNodes + cells * s.overheadPerCell + mgmtNodes(s).nodes;  // M8: mgmt nodes DERIVED (was flat fixedNodes)

  // M16: pricingModel scales EC2 compute (Compute SP covers EC2, not the EKS $73 fee / RDS). Default 'ri' → ×1.0.
  const pricingMult = PRICING_MULT[s.pricingModel] ?? 1.0;
  const compute = P.ec2Node * totalNodes * pricingMult + P.eksCluster * cells;
  const rdsAll = P.rdsMem * Q.rdsMemN + P.rdsRag * Q.rdsRagN + P.rdsLens * Q.rdsLensN
    + P.rdsProm * Q.rdsPromN + P.rdsTrust * Q.rdsTrustN;
  const controlPlane = P.eksCluster * 2 + rdsAll + P.rdsProxy * Q.rdsProxyN + P.cacheNode * Q.cacheN
    + P.albMonth * Q.albN + P.natGW * Q.natN;
  const t = traffic(s);   // M13: demand-driven CloudFront/NAT + data transfer
  const natMult = s.gatewayEndpoints ? (1 - GATEWAY_NAT_REDUCTION_FRAC) : 1;   // M16: gateway endpoints keep S3/ECR off NAT
  const global = P.rdsStoreGB * Q.rdsStoreGB + P.s3GB * Q.s3GB + P.ecrGB * Q.ecrGB
    + P.cfEgressGB * t.cfEgressGB + P.cfReqM * t.cfRequestsM + P.natGB * t.natGB * natMult
    + P.dtCrossAzGB * t.crossAzGB
    + P.waf * Q.wafN + P.r53Zone * Q.r53N + P.kmsKey * Q.kmsN + P.smSecret * Q.smN;

  const regions = Math.max(1, s.regions || 1);
  const computeMult = s.crossRegionReplication ? regions : 1;   // active-active replicates compute
  const controlMult = regions > 1 ? regions : 1;                // every region needs its own control plane
  const total = global + compute * computeMult + controlPlane * controlMult + P.dtInterRegionGB * t.interRegionGB;
  return { total, compute, controlPlane, global, totalNodes, cells };
}

// Decision-support table: single vs sharded vs active-active — cost + blast radius + SLA + score.
export function comparePostures(s = DEFAULTS, opt = {}) {
  const regions = Math.max(2, opt.regions || (s.regions > 1 ? s.regions : 3));
  const row = (posture, over) => {
    const sc = scn({ ...s, ...over });
    const rk = chaos(sc, { killRegion: true });
    const raw = cost(sc).total;
    return {
      posture, regions: sc.regions, replication: !!sc.crossRegionReplication,
      costPerMonth: Math.round(raw), _raw: raw, regionBlastRadiusPct: rk.capLossPct,
      sla: rk.sla, reviewScore: review(sc).score, costOfResilienceVsSingle: 0,
    };
  };
  const single = row('single', { regions: 1, crossRegionReplication: false });
  const sharded = row('sharded', { regions, crossRegionReplication: false });
  const aa = row('active-active', { regions, crossRegionReplication: true });
  // Δ from UNROUNDED totals (then round) — avoids ±1 drift from rounding both sides first.
  sharded.costOfResilienceVsSingle = Math.round(sharded._raw - single._raw);
  aa.costOfResilienceVsSingle = Math.round(aa._raw - single._raw);
  for (const r of [single, sharded, aa]) delete r._raw;
  return [single, sharded, aa];
}

// Line-item breakdown (the calculator's view) — subtotals SUM EXACTLY to cost(s).total. Region
// scaling matches cost(): compute lines ×computeMult, control-plane lines ×controlMult, global ×1.
// This is the single source the engine-backed calculator renders (no independent price table).
export function costBreakdown(s = DEFAULTS) {
  const P = PRICES, Q = COST_QTY;
  const cells = ceilDiv(s.tenants, s.maxPerCell);
  const activePods = s.tenants * s.activeFraction * s.podsPerTenant;
  const { ppn } = podsPerNode(s);
  const appNodes = ceilDiv(activePods, ppn);
  const totalNodes = appNodes + cells * s.overheadPerCell + mgmtNodes(s).nodes;  // M8: mgmt nodes DERIVED (was flat fixedNodes)
  const regions = Math.max(1, s.regions || 1);
  const cm = s.crossRegionReplication ? regions : 1;   // compute mult
  const km = regions > 1 ? regions : 1;                 // control-plane mult
  const t = traffic(s);                                 // M13: demand-driven edge + data transfer
  const pricingMult = PRICING_MULT[s.pricingModel] ?? 1.0;              // M16: EC2 compute vehicle
  const natMult = s.gatewayEndpoints ? (1 - GATEWAY_NAT_REDUCTION_FRAC) : 1;   // M16: gateway endpoints
  // M17: every cost line is a CAPSIM projection (Vantage prices × modelled capacity — never a measured bill).
  const L = (group, id, label, qty, unitPrice, mult) => ({ group, id, label, qty, unitPrice, mult, subtotal: qty * unitPrice * mult, fidelity: 'CAPSIM' });
  return [
    L('COMPUTE', 'ec2', 'EC2 nodes', totalNodes, P.ec2Node * pricingMult, cm),
    L('COMPUTE', 'eks_cells', 'EKS cells', cells, P.eksCluster, cm),
    L('COMPUTE', 'eks_fixed', 'EKS mgmt+build', 2, P.eksCluster, km),
    L('DATA', 'rdsMem', 'RDS memory', Q.rdsMemN, P.rdsMem, km),
    L('DATA', 'rdsRag', 'RDS rag', Q.rdsRagN, P.rdsRag, km),
    L('DATA', 'rdsLens', 'RDS lens', Q.rdsLensN, P.rdsLens, km),
    L('DATA', 'rdsProm', 'RDS promptly', Q.rdsPromN, P.rdsProm, km),
    L('DATA', 'rdsTrust', 'RDS trust', Q.rdsTrustN, P.rdsTrust, km),
    L('DATA', 'rdsProxy', 'RDS Proxy', Q.rdsProxyN, P.rdsProxy, km),
    L('DATA', 'cache', 'ElastiCache', Q.cacheN, P.cacheNode, km),
    L('EDGE', 'alb', 'ALB', Q.albN, P.albMonth, km),
    L('EDGE', 'nat', 'NAT Gateway', Q.natN, P.natGW, km),
    L('DATA', 'rdsStore', 'RDS storage (GB)', Q.rdsStoreGB, P.rdsStoreGB, 1),
    L('DATA', 's3', 'S3 (GB)', Q.s3GB, P.s3GB, 1),
    L('DATA', 'ecr', 'ECR (GB)', Q.ecrGB, P.ecrGB, 1),
    L('EDGE', 'cfEgress', 'CloudFront egress (GB)', t.cfEgressGB, P.cfEgressGB, 1),
    L('EDGE', 'cfReq', 'CloudFront requests (M)', t.cfRequestsM, P.cfReqM, 1),
    L('EDGE', 'natGB', 'NAT data (GB)', t.natGB, P.natGB, natMult),
    L('EDGE', 'crossAz', 'Cross-AZ transfer (GB)', t.crossAzGB, P.dtCrossAzGB, 1),
    L('EDGE', 'interRegion', 'Inter-region transfer (GB)', t.interRegionGB, P.dtInterRegionGB, 1),
    L('EDGE', 'waf', 'WAF', Q.wafN, P.waf, 1),
    L('EDGE', 'r53', 'Route53 zones', Q.r53N, P.r53Zone, 1),
    L('SECURITY', 'kms', 'KMS keys', Q.kmsN, P.kmsKey, 1),
    L('SECURITY', 'sm', 'Secrets Manager', Q.smN, P.smSecret, 1),
  ];
}
