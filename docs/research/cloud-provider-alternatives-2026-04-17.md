# Cloud Provider Alternatives — Research (2026-04-17)

> Research snapshot. The cloud market moves fast — validate pricing before citing.
>
> **Update 2026-04-17 (price correction):** Initial web research overstated Vultr savings. Real Vultr K8s pricing (General Purpose tier) is ~20% MORE expensive than DO, not cheaper. See corrected tables below.

## Context

LoopZero runs on DigitalOcean (3 DOKS clusters, managed PG, DO Spaces). The following limits are blocking scale validation:

| DO Limitation | Impact on LoopZero |
|---------------|----------------|
| Droplet quota (default low, requires support ticket) | Blocking scale gates #1 and #4 — literally waiting on ticket |
| Max 512 nodes/cluster, 64 nodes/pool | Cell architecture needs multiple clusters anyway, but ceiling is real |
| DOKS control plane not configurable | No feature gates, no custom admission controllers |
| DOCR 100GB cap | Already mitigated (migrated to Harbor), but was painful |
| No IPv6 on DOKS | Minor today, relevant for future |
| Upgrade surge consumes Droplet quota | Cluster upgrades compete with production quota |

---

## Provider Comparison

### AWS EKS Actual Pricing (verified — reference benchmark)

Source: AWS EKS pricing page (April 2026). **Control plane is NOT free.**

| Component | Pricing |
|-----------|--------:|
| EKS cluster (standard K8s version) | $0.10/cluster/hr → **$73/mo per cluster** |
| EKS cluster (extended K8s version) | $0.60/cluster/hr → **$438/mo per cluster** |
| EKS Provisioned Control Plane XL | $1.65/cluster/hr → **$1,204/mo per cluster** (+ base cluster fee) |
| EKS Auto Mode (per-node surcharge) | On top of EC2 instance price |
| EKS ArgoCD Capability (base) | $0.03/hr → **$21.90/mo per cluster** |
| EKS ArgoCD Capability (per app) | $0.0015/app/hr → **$1.10/mo per app** |

**LoopZero on AWS EKS (3 clusters, equivalent nodes, 50 ArgoCD apps):**

| Item | Monthly Cost |
|------|------------:|
| 3x EKS control planes (standard) | $219.00 |
| 6x EC2 nodes (~m6a.large 2vCPU/8GB, on-demand) | ~$450.00 |
| RDS PostgreSQL (db.t4g.medium, single-AZ) | ~$65.00 |
| ArgoCD Capability (3 clusters × base) | $65.70 |
| ArgoCD Capability (50 apps) | $55.00 |
| Data transfer (egress) | ~$50-100 |
| **Total estimated** | **~$905-$955/mo** |

> AWS is 2.5-3x more expensive than DO for LoopZero's current setup. The control plane fee alone ($219/mo for 3 clusters) exceeds Hetzner's entire compute budget. ArgoCD as a managed EKS Capability adds another ~$120/mo for what we run self-hosted for free. AWS only makes sense at enterprise scale where EC2 Reserved Instances / Savings Plans bring 40-60% discounts.

---

### GCP GKE Actual Pricing (verified — reference benchmark)

Source: GKE pricing page (April 2026). Control plane: $0.10/cluster/hr (same as EKS).

| Component | Pricing |
|-----------|--------:|
| GKE cluster management fee | $0.10/cluster/hr → **$73/mo per cluster** |
| Free tier credit | **$74.40/mo** per billing account (covers ~1 zonal/Autopilot cluster) |
| Autopilot compute | Pay per pod CPU/memory (no idle node waste) |
| Standard compute | Pay for Compute Engine node instances |

**LoopZero on GKE (3 clusters, 6 nodes — current equivalent):**

| Item | Monthly Cost |
|------|------------:|
| 3x GKE control planes | $219.00 |
| Free tier credit | -$74.40 |
| 6x Compute Engine nodes (~e2-standard-2, 2vCPU/8GB, on-demand) | ~$360.00 |
| Cloud SQL PostgreSQL (db-custom-2-8192, single-zone) | ~$75.00 |
| Data transfer (egress, 1TB) | ~$85.00 |
| **Total estimated** | **~$665/mo** |

**LoopZero on GKE (3 clusters, 12 nodes — scale scenario, verified via GCP calculator):**

| Item | Monthly Cost |
|------|------------:|
| 3x GKE control planes | $219.00 |
| Free tier credit | -$74.40 |
| 12x n4-standard-4 (4vCPU/16GB, Iowa, on-demand, COS) | ~$1,604/mo |
| Cloud SQL PostgreSQL (db-custom-4-16384, HA) | ~$150.00 |
| Data transfer (egress, 2TB) | ~$170.00 |
| **Total estimated (on-demand)** | **~$2,069/mo** |
| **Total estimated (1yr CUD, -37%)** | **~$1,375/mo** |
| **Total estimated (3yr CUD, -55%)** | **~$1,070/mo** |

> Source: GCP pricing calculator verified April 2026. R$ 9,813.57/mo for 12x n4-standard-4 with Ubuntu Pro = ~$1,736/mo. With standard COS (Container-Optimized OS, free) instead of Ubuntu Pro ($11/node/mo), drops to ~$1,604/mo.
>
> **Equivalent 12 nodes on DO:** ~$1,152/mo (12x 4vCPU/16GB Droplets at ~$96/ea). GCP is **+50% on-demand**, but with 3yr CUD narrows to **-7% vs DO** — nearly break-even, but with contract lock-in.

**Key observations:**
- GKE is ~90% more expensive than DO at current 6-node scale (on-demand)
- At 12-node scale, the gap widens to +70% (on-demand) but CUDs narrow it significantly
- The free tier credit ($74.40) covers ~1 cluster — irrelevant at 3-cluster architecture
- Egress at ~$0.085/GB is a hidden cost that DO/Civo/Hetzner don't have
- **Ubuntu Pro adds ~$132/mo for 12 nodes** — use COS or standard Ubuntu instead
- GKE Autopilot is interesting (pay-per-pod, no idle waste) but adds constraints on pod specs
- **GKE advantage:** Most mature K8s (Google built K8s), Autopilot eliminates node management, 40+ regions
- **GKE disadvantage:** Cost structure penalizes multi-cluster architectures like LoopZero's

---

### Vultr K8s Actual Pricing (General Purpose — verified)

Source: Vultr Kubernetes Engine pricing page (April 2026). Control plane is free. Worker nodes:

| vCPUs | Memory | Bandwidth | Storage | Monthly |
|:-----:|:------:|:---------:|:-------:|--------:|
| 1 | 4 GB | 4 TB | 30 GB | $30.00 |
| 2 | 8 GB | 5 TB | 50 GB | $60.00 |
| 4 | 16 GB | 6 TB | 80 GB | $120.00 |
| 8 | 32 GB | 7 TB | 160 GB | $240.00 |
| 16 | 64 GB | 8 TB | 320 GB | $480.00 |
| 24 | 96 GB | 9 TB | 480 GB | $720.00 |
| 32 | 128 GB | 9 TB | 640 GB | $960.00 |

> **Warning:** Web comparison articles often cite Vultr's "Cloud Compute" (shared vCPU) pricing, which is cheaper. The K8s General Purpose tier above uses dedicated vCPUs and is ~20% MORE expensive than DO equivalents.

### Cost Estimate for LoopZero (3 clusters x 2 nodes medium + managed PG)

| Provider | 6 nodes (2vCPU/8GB) | PG Managed | Egress | Control Plane (3 clusters) | Total Est. | vs DO | Regions |
|----------|:--------------------:|:----------:|:------:|:--------------------------:|:----------:|:-----:|:-------:|
| **AWS EKS** | ~$450/mo | ~$65/mo | ~$75/mo | **$219/mo** | **~$905/mo** | +159% | 33 regions |
| **GCP GKE** | ~$360/mo | ~$75/mo | ~$85/mo | **$145/mo** (after credit) | **~$665/mo** | +90% | 40+ regions |
| **Vultr** | ~$360/mo | ~$60/mo | Included | Free | **~$420/mo** | +20% | 32 DCs |
| **DigitalOcean** | ~$288/mo | ~$60/mo | Included (limited) | Free | **~$350/mo** | — | 14 DCs |
| **OVHcloud** | ~$220/mo (b3-8) | ~$55/mo | **Free (EU+NA)** | Free | **~$275/mo** | -21% | 15 DCs |
| **Scaleway** | ~$180/mo | ~$45/mo | Free (EU) | Free | **~$230/mo** | -34% | 3 DCs |
| **Civo** | ~$160/mo | ~$50/mo | **Free unlimited** | Free | **~$210/mo** | -40% | 3 DCs |
| **Hetzner** | ~$108/mo | N/A (self-host) | 20TB free (EU) | N/A (self-host K8s) | **~$130/mo** + ops | -63% | 5 DCs |

> **Key takeaway:** Hyperscalers (AWS, GCP) are 2-3x more expensive due to control plane fees and egress charges. Mid-tier providers (DO, Vultr) cluster around $350-420/mo. EU-focused providers (OVH, Scaleway, Civo) are 20-40% cheaper with free egress. Hetzner is cheapest but requires self-managing K8s + PG + registry. DO sits in the sweet spot of cost vs managed services maturity.

**Scale scenario: 12 nodes (4vCPU/16GB) + PG HA — production target**

| Provider | 12 nodes | PG HA | Egress | Control Plane (3 clusters) | Total Est. | vs DO |
|----------|:--------:|:-----:|:------:|:--------------------------:|:----------:|:-----:|
| **AWS EKS** | ~$900/mo | ~$130/mo | ~$150/mo | $219/mo | **~$1,400/mo** | +60% |
| **GCP GKE (on-demand)** | ~$1,604/mo | ~$150/mo | ~$170/mo | $145/mo | **~$2,069/mo** | +137% |
| **GCP GKE (3yr CUD)** | ~$720/mo | ~$150/mo | ~$170/mo | $145/mo | **~$1,070/mo** | +22% |
| **Vultr** | ~$720/mo | ~$120/mo | Included | Free | **~$840/mo** | -4% |
| **DigitalOcean** | ~$1,152/mo | ~$120/mo | Included | Free | **~$872/mo** | — |
| **OVHcloud** | ~$440/mo | ~$110/mo | Free (EU+NA) | Free | **~$550/mo** | -37% |
| **Civo** | ~$480/mo | ~$100/mo | **Free unlimited** | Free | **~$580/mo** | -33% |
| **Hetzner** | ~$216/mo | N/A (self-host) | 20TB free | N/A (self-host K8s) | **~$260/mo** + ops | -70% |

> At scale (12 nodes), the cost gap narrows between mid-tier providers. Vultr becomes nearly price-equivalent to DO. Hyperscalers remain significantly more expensive on-demand, but GCP with 3yr CUD gets within 22% of DO. EU providers (OVH, Civo) remain 30-37% cheaper.

---

## Provider Analysis

### 1. Hetzner — Cheapest Raw Compute (~60% less than DO)

**Pros:**
- ~60% cheaper than DO in raw compute
- 20TB egress free (EU)
- NVMe everywhere, ARM (CAX) unbeatable in cost/vCPU
- Ampere CAX is the value champion across all benchmarks

**Cons:**
- **No managed Kubernetes** — requires Edka, k3s, or Syself
- **No managed database** — self-host PG or use Ubicloud
- **No container registry**
- Limited DCs (EU + 1 US + 1 SG)
- Basic support, no strong SLA
- 15-30% price increase effective April 2026

**Managed K8s options on Hetzner:**
- [Cloudfleet](https://cloudfleet.ai/lp/managed-hetzner-kubernetes/) — free Basic plan with up to 24 vCPUs, 99.95% SLA
- [Syself Autopilot](https://syself.com/hetzner) — fully automated, GitOps-compatible, 100% reproducible clusters

**Verdict:** Maximum savings, but brings back the ops overhead that LoopZero abstracts for customers. Makes sense if the team has SRE bandwidth. Ironic: we'd sell "no Kubernetes" while managing raw K8s internally.

### 2. Vultr — Best Hardware, But MORE Expensive

**Pros:**
- **2.5x better single-core** than DO (EPYC-Genoa vs Xeon)
- Managed K8s (VKE) + Managed PG — same paradigm as DO
- **32 regions** (best global reach of all candidates)
- Container registry included
- Bare metal node option for K8s

**Cons:**
- **~20% MORE expensive** than DO for K8s General Purpose nodes (verified)
- Ecosystem less polished than DO
- Managed K8s less mature than DOKS
- Fewer tutorials/docs
- No App Platform or strong integrations
- Smaller community

**Benchmarks (March 2026):**
- Single-core Geekbench: Vultr EPYC-Genoa 1,926 vs DO Intel Xeon 772
- 4K IOPS: Vultr ~118k vs DO ~54k

**Verdict:** Better hardware but costs more. Only justified if we need 32 regions for cell architecture or if performance bottlenecks on DO become a problem. NOT a cost savings move. Pulumi migration would be straightforward if we decide to go.

### 3. Civo — K3s Native, Developer-Friendly

**Pros:**
- Clusters in **<90 seconds**
- **Egress 100% free**
- K3s-based (lightweight, aligns with our k3d local dev)
- UK/EU compliance (ISO27001, G-Cloud)
- Marketplace with one-click add-ons
- $250 free credit to test

**Cons:**
- **Only 3 regions** (LON, FRA, NYC)
- Younger platform
- Fewer enterprise features
- No robust managed PG
- Limited GPU

**Verdict:** Interesting for EU/compliance. K3s aligns with local k3d. But 3 regions is limiting for global cell architecture.

### 4. OVHcloud — Europe, No Egress Fees

**Pros:**
- **Egress free** (EU+NA)
- Free control plane, up to 500 nodes
- GDPR-native (data stays in EU)
- Savings Plans available
- Standard plan: 99.99% SLA (multi-AZ control plane)
- Free 1-month trial of Standard plan

**Cons:**
- UX/API less polished
- Less automation/tooling
- Limited ecosystem
- Smaller dev community

**Verdict:** Strong for EU data residency. Competitive pricing. But the ecosystem is weak — less integration, less tooling, more friction day-to-day.

### 5. Scaleway — Europe, Developer-Friendly

**Pros:**
- Free control plane
- GDPR-compliant
- Kapsule well integrated
- Container Registry included
- Object Storage with free egress

**Cons:**
- **Only 3 regions** (PAR, AMS, WAW)
- Smaller ecosystem
- No strong US presence

**Verdict:** Similar to Civo but EU-only. Good if target is Europe, limiting for US.

---

## Decision Matrix

| Criterion (weight) | DO (current) | Hetzner | Vultr | Civo | OVHcloud |
|--------------------|:------------:|:-------:|:-----:|:----:|:--------:|
| Monthly cost (25%) | 3/5 | **5/5** | 2/5 | 4/5 | 3/5 |
| Managed K8s maturity (20%) | **4/5** | 1/5 | 3/5 | 3/5 | 3/5 |
| Managed PG (15%) | **4/5** | 0/5 | 3/5 | 2/5 | 3/5 |
| Regions/global reach (10%) | 3/5 | 2/5 | **5/5** | 1/5 | 3/5 |
| Quotas/limits (15%) | 1/5 | **4/5** | 3/5 | 3/5 | 4/5 |
| Migration effort (15%) | **5/5** | 1/5 | 4/5 | 3/5 | 2/5 |
| **Weighted score** | **3.35** | **2.50** | **3.10** | **2.75** | **2.95** |

> With corrected pricing, DO actually scores higher than Vultr. The main reason to leave DO is quotas/limits, not cost.

---

## Recommendation (Updated with Real Pricing)

**No provider is a clear cost win over DO.** The real question is: are we migrating for cost, or for limits?

### If migrating for COST: Hetzner (with caveats)

- ~60% cheaper in raw compute, but requires self-hosting K8s + PG + registry
- Hidden ops cost is real — we'd manage what we abstract for customers
- Only viable if team has dedicated SRE bandwidth
- Best paired with Cloudfleet or Syself for managed K8s layer

### If migrating for LIMITS: Vultr (but costs more)

- 32 regions, better hardware (EPYC-Genoa, NVMe)
- Same paradigm (managed K8s + managed PG) — simplest migration path
- **~20% MORE expensive** than DO, not cheaper
- Justified only if DO quotas become a recurring blocker or cell architecture needs 32 regions

### If migrating for EU COMPLIANCE: Civo or OVHcloud

- Civo: K3s-native, free egress, ISO27001, but only 3 regions
- OVHcloud: GDPR-native, 99.99% SLA, 15 DCs, but weaker ecosystem

### Pragmatic recommendation: STAY on DO, fight for quotas

Given that:
1. Vultr is more expensive, not cheaper
2. Hetzner saves money but adds ops burden
3. Civo/OVHcloud/Scaleway have limited regions
4. Migration is T2+ (Pulumi, CI/CD, DNS, secrets, registry, PG)

The most pragmatic path is:
- **Escalate DO quota ticket aggressively** (support ticket + account manager)
- **Request dedicated account manager** if not already assigned
- **Document quota needs** for next 6-12 months to pre-request increases
- **Revisit if DO consistently fails to meet quota needs** — then Vultr for limits or Hetzner for cost

### DO Partner Network Connect (multi-cloud option)

DO offers private multi-cloud connectivity via Megaport (Partner Network Connect):

| Throughput | Monthly Cost |
|:----------:|------------:|
| 1 Gbps | $840/mo |
| 2 Gbps | $1,540/mo |
| 5 Gbps | $2,520/mo |
| 10 Gbps | $3,850/mo |

Supports connecting DO VPC to AWS, GCP, and Azure. Now supports HA (redundant links) and DOKS clusters. Egress over Partner Network Connect is not charged by DO (but Megaport and the other cloud provider charge separately).

> **Verdict for LoopZero:** Way too expensive for current stage ($840/mo for 1Gbps > our entire infra budget). Only relevant for Enterprise tier if BYOC customers need private connectivity between their cloud and our DO control plane. File under "future capability awareness", not "current action item".

### If we DO decide to migrate

This is a **T2 change minimum** (Pulumi, CI/CD, DNS, secrets, registry, PG migration). Before implementation:

1. PoC on target provider: 1 cluster + PG + deploy test
2. Compare real K8s quotas/limits vs DOKS
3. Formal meeting (T2) for decision
4. Pulumi provider abstraction (already designed via ISP contracts) should make the swap manageable

---

## Competitor Infrastructure Map

Where competitors actually run their platforms:

| Platform | Underlying Infra | Model | Data Centers | Notes |
|----------|-----------------|-------|:------------:|-------|
| **Vercel** | AWS (abstracted) | Fully managed SaaS | Edge global | Users never see AWS |
| **Railway** | **Own bare metal** (migrated from GCP 2024) | Own co-location | 4 DCs (US, EU, Asia) | $100M Series B funded the migration |
| **Render** | AWS (EU) + GCP (US) | Multi-cloud managed | 2 regions | Only major PaaS still on hyperscalers |
| **Fly.io** | **Own bare metal** (always was) | Own servers, Firecracker VMs | 35+ DCs | Built on custom hardware from day 1 |
| **Northflank** | Multi-cloud + BYOC | AWS/GCP/Azure/Civo/Oracle/bare-metal | 6+ managed, 600+ BYOC | Most flexible model |
| **Qovery** | **BYOC** (customer's cloud) | Control plane only | AWS/GCP/Azure/Scaleway | Rust engine + Terraform |
| **Civo** | **Own DCs** (UK) + co-location | Own hardware, K3s | 4 DCs (LON×2, FRA, NYC) | UK sovereign cloud, expanding to EU/Asia |
| **Coolify** | **Any server** (self-hosted) | SSH to your server | Any | 40k GitHub stars |
| **Koyeb** | Was own infra → **Acquired by Mistral AI** | Pivoted | 25+ edge (was) | Now AI/ML focused |
| **LoopZero** | **DigitalOcean** (managed) | DOKS + managed PG + Spaces | SFO3 (dev), NYC3 (prod) | 3 clusters, CF edge |

### Key patterns

**1. Market leaders are on own hardware.** Railway (migrated from GCP 2024), Fly.io (always bare metal), Civo (own UK DCs). Motivation: margin control, performance differentiation, independence from hyperscaler outages.

**2. BYOC is the enterprise play.** Northflank (600+ BYOC regions) and Qovery (Rust engine on customer's cloud). Eliminates vendor lock-in, leverages customer's existing cloud credits.

**3. Render is the exception.** Only major PaaS still running on hyperscalers (AWS EU + GCP US) without own hardware.

**4. Self-hosted dominates open source.** Coolify (40k stars), Dokploy (15k), Dokku (29k) — all run on whatever server the user provides.

### LoopZero's natural evolution path

| Phase | Model | Example | When |
|-------|-------|---------|------|
| **Now** | Managed cloud (DO) | Render | Pre-release, validate product-market fit |
| **Growth** | Multi-cloud via Pulumi ISP contracts | Northflank managed | When DO limits become recurring blocker |
| **Enterprise** | BYOC (deploy on customer's cloud) | Qovery, Northflank BYOC | Enterprise tier demand |
| **Scale** | Own bare metal (co-location) | Railway, Fly.io | When margin justifies capex ($2M+ cloud bill) |

> The Pulumi ISP contracts architecture (already designed) enables phases 2-3. Phase 4 requires capital investment and dedicated SRE team — not relevant until post-product-market-fit.

---

## Sources

- [Kubernetes Limits | DigitalOcean](https://docs.digitalocean.com/products/kubernetes/details/limits/)
- [Droplet Limits | DigitalOcean](https://docs.digitalocean.com/products/droplets/details/limits/)
- [Hetzner Cloud Review 2026 | Better Stack](https://betterstack.com/community/guides/web-servers/hetzner-cloud-review/)
- [Hetzner Kubernetes Complete Guide 2026 | Cloudfleet](https://cloudfleet.ai/blog/cloud-native-how-to/2026-04-hetzner-kubernetes-complete-guide/)
- [Is Hetzner Worth It 2026 | jcalloway.dev](https://jcalloway.dev/is-hetzner-worth-it-2026-honest-developer-review-after-2-years)
- [Hetzner Price Adjustment April 2026 | UBOS](https://ubos.tech/news/hetzner-price-adjustment-updated-cloud-costs-effective-april-2026/)
- [Vultr vs DigitalOcean 2026 | Better Stack](https://betterstack.com/community/guides/web-servers/digitalocean-vs-vultr/)
- [Vultr vs DigitalOcean 2026 | Utho](https://utho.com/blog/vultr-vs-digitalocean/)
- [DigitalOcean Managed Database Pricing](https://www.digitalocean.com/pricing/managed-databases)
- [Civo Kubernetes | Gartner Peer Insights](https://www.gartner.com/reviews/product/civo-kubernetes)
- [Civo Pricing](https://www.civo.com/pricing)
- [AWS vs Civo Benchmark | kubeblogs](https://www.kubeblogs.com/aws-charges-extra-for-storage-performance-that-civo-ships-by-default/)
- [Scaleway Kubernetes Kapsule](https://www.scaleway.com/en/kubernetes-kapsule/)
- [OVHcloud Managed Kubernetes](https://www.ovhcloud.com/en/public-cloud/kubernetes/)
- [OVHcloud Kubernetes Review | EU Cloud Cost](https://www.eucloudcost.com/blog/ovhcloud-cluster/)
- [European Kubernetes Providers Compared 2026 | EU Cloud Cost](https://www.eucloudcost.com/kubernetes/compare/)
- [K8s: Hetzner vs Civo vs DigitalOcean | sanj.dev](https://sanj.dev/post/kubernetes-hetzner-cloud-2025)
- [10 Best DO Alternatives 2026 | Northflank](https://northflank.com/blog/best-digitalocean-alternatives-2026)
- [Top 10 Managed Kubernetes Providers 2026 | Dysnix](https://dysnix.com/blog/top-managed-kubernetes-providers)
- [Railway $100M to challenge AWS | VentureBeat](https://venturebeat.com/infrastructure/railway-secures-usd100-million-to-challenge-aws-with-ai-native-cloud)
- [Railway turns heads with speed | TechTarget](https://www.techtarget.com/searchcloudcomputing/news/366637659/Upstart-cloud-provider-Railway-turns-heads-with-speed)
- [Fly.io Architecture | Fly Docs](https://fly.io/docs/reference/architecture/)
- [Fly.io investments | Intel Capital](https://www.intelcapital.com/the-compute-layer-for-the-modern-distributed-cloud-our-investments-in-fly-io/)
- [Northflank Cloud Providers](https://northflank.com/cloud)
- [Northflank Regions](https://northflank.com/cloud/northflank/regions)
- [Qovery Engine | GitHub](https://github.com/Qovery/engine)
- [Civo UK Sovereign Cloud](https://www.civo.com/uk-sovereign-cloud)
- [Civo second UK region with Deep Green | DCD](https://www.datacenterdynamics.com/en/news/civo-launches-second-uk-cloud-region-with-deep-green/)
- [Coolify Introduction | Docs](https://coolify.io/docs/get-started/introduction)
