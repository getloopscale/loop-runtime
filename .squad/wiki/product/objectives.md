# Objectives: loopzero

Agreed in the brainstorm session of 2026-10-04 and 2026-10-05
(`.squad/records/brainstorms/2026-10-04-session.md`). Phase 2 of 4; not yet signed.

Production launch is 2026-11-01. Every objective is judged twice: its dogfooding criterion on
2026-11-01, as a condition to launch, and its production criterion on 2027-01-30, 90 days after
launch. The dates are reviewed at each milestone delivery (F0 to F4 of the "Production readiness"
Project), and a moved date is recorded with its reason rather than overwritten.

Every threshold below is a hypothesis, to revisit after the first month of production data.

## OBJ-1 - A team with no platform engineer ships its first agent alone, in its first session
metric: in production, at least 60% of new teams that run `loopzero deploy` reach an active release with an HTTPS URL within 24 hours of creating the account, and their median time from account creation to first active release is under 30 minutes (SQL over `tenants.created_at` and the releases' `created_at` and `status`). In dogfooding, 5 of 5 runs pass, each from a new account on a clean machine with a sample LoopZeroKit project of at least two agents, following only the public documentation, from `loopzero login` to the agents answering over HTTPS in 30 minutes or less; at least 2 of the 5 are run by someone outside the team.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: the vision's user has nobody on platform work, so a first deploy that needs our help fails them at the first step. "Without a ticket" has no measure yet and is counted by hand until support is a recorded channel.

## OBJ-2 - Deploy and rollback are routine, and a bad release goes back before users notice
metric: 100% of rollbacks end `ROLLED_BACK`, with zero `ROLLBACK_FAILED`; p95 of at most 2 minutes from `loopzero rollback` to the previous release serving again; in 100% of failed deploys (canary aborted, health check failed) the previous release keeps serving. Dogfooding breaks one release on purpose per run. Rollback time is read from the CLI in dogfooding, because `releases` records no transition timestamp yet.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: the vision names deploys and rollbacks treated as risky events as part of the cost this team pays today.

## OBJ-3 - The bill never surprises: it stays under the team's own ceiling, and idle costs nothing
metric: zero invoices above the ceiling the team set, over every invoice in the period; a service at zero replicas is billed zero compute-hours (time at zero replicas crossed with metered hours, difference 0); 100% of teams that cross 80% of their ceiling get the alert before they reach 100%. Dogfooding sets a low ceiling in each run and forces usage past it; the run passes if the alert arrives, usage stops at the ceiling and the simulated invoice stays under it. Under BYOC the ceiling covers only what LoopZero charges.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: fear of the monthly bill is the third cost in the vision, and usage-based pricing with a ceiling is part of what the product is.

## OBJ-4 - A team runs its agents in its own cloud account, isolated, with the same `loopzero deploy` and none of its long-lived credentials on our side
metric: the customer runs exactly one artifact in their account (the IAM bootstrap) and no manual Kubernetes or network step, counted on the onboarding script; 60 minutes or less from running the bootstrap to the first agent answering over HTTPS in the customer's account; a scan of our secret stores and database finds only a role ARN and an `ExternalId` per customer account and zero access keys; only platform telemetry leaves the customer's account, checked on the cell's egress flows. Dogfooding uses a separate member account of our AWS organization acting as the customer.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: BYOC ships at launch for the same user, who needs agents and data on infrastructure they own; each cell is a Cluster API object (loopzero#490, option B) and access uses AssumeRole with ExternalId (CAPI proposal D5).

## OBJ-5 - An agent uses every managed service through one key and one MCP endpoint, with nothing extra to run
metric: in dogfooding, 5 of 5 runs pass, each with the sample project's agents calling all eight managed services (memory, document retrieval, context, prompts, skills, trust policies, file storage, and traces in Lens) with the account's single key through its single MCP endpoint; every call succeeds, the execution's trace appears in Lens, no step falls outside `loopzero` and the agent's code, and 15 minutes or less pass from the first deploy to the last call. In production, at least 40% of active teams call at least one managed service in their first 30 days; each service fails under 0.1% of the month's calls with an error on our side (5xx); and zero accesses by one team to another team's data happen in any service.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: the vision now promises the managed services in one account with no extra infrastructure, and the sponsor defined the product as the whole ecosystem on 2026-10-05; the three production numbers say whether teams use the services, whether they work, and whether they stay isolated.

## OBJ-6 - The platform runs in production safely and recovers from failure
metric: in dogfooding, every image the release publishes has zero fixable HIGH or CRITICAL vulnerabilities at scan, a secret scan of every repository's full history finds no live credential, an external penetration test closes with no critical finding open, and a restore drill on staging brings back the database and the Git repositories losing at most 1 hour of data and within 4 hours. In production, monthly availability of the API measured by an external probe is at least 99.5%, zero credentials are exposed, and every paging alert reaches a person.
horizon: dogfooding judged 2026-11-01; production judged 2027-01-30
why: tracing the migrated backlog on 2026-10-05 found 27 launch-blocking items (a live cloud token in git history, unrotated keys, images with dozens of fixable HIGH vulnerabilities, no backup schedule, no runbooks, no production environment on AWS) that served none of the five objectives, because those measure what the customer does and none measured whether the platform itself can run in production.
