# Technical pieces: loopzero

Agreed in the brainstorm session of 2026-10-05
(`.squad/records/brainstorms/2026-10-04-session.md`). Phase 4 of 4; not yet signed.

A piece is a responsibility with a boundary, not a repository or a service. The mapping to
repositories is decided later by `/backlog-init`, from disk. Where two pieces realise one
requirement, the split is stated in their responsibilities.

## PIECE-1 - Identity and accounts
realises: REQ-1, REQ-23
responsibility: sign-up, login and teams, the single key each account uses for every managed service, plus the audit log of administrative actions that shows a new account needed nobody on our side.

## PIECE-2 - CLI
realises: REQ-1, REQ-2, REQ-4, REQ-11, REQ-16, REQ-18
responsibility: the user's interface: every command, error messages in product language, the contract exit codes, the ceiling and usage commands, and the choice of target per environment.

## PIECE-3 - Project intake
realises: REQ-2
responsibility: recognizing a LoopZeroKit project through the `.loopzero/services.json` seam and validating the bundle a deploy uploads.

## PIECE-4 - Build
realises: REQ-3, REQ-4, REQ-6
responsibility: producing and signing the artifact of every agent and service, and keeping the build history that shows a rollback started no build.

## PIECE-5 - Release orchestration
realises: REQ-6, REQ-7, REQ-8, REQ-9, REQ-15
responsibility: the release lifecycle: the per-environment lock, progressive delivery and its abort decision, rollback with no rebuild, the time of every transition, and refusing a deploy once metering says the ceiling is reached.

## PIECE-6 - Runtime and edge
realises: REQ-3, REQ-7, REQ-12, REQ-21, REQ-24
responsibility: running the workloads, giving each agent its HTTPS URL and, at deploy, the endpoint and key of the managed services, keeping the previous release serving while a new one is aborted, scaling an idle service to zero and waking it on the first request, and separating one customer's workloads from another's.

## PIECE-7 - Preview environments
realises: REQ-10
responsibility: creating a preview of the whole project when a pull request opens and removing it when the pull request closes.

## PIECE-8 - Metering and billing
realises: REQ-11, REQ-13, REQ-14, REQ-15, REQ-16
responsibility: metering usage per deployed service and per managed-service operation, holding the team's ceiling, alerting at 80%, deciding the ceiling is reached and stopping billable usage, and projecting the invoice.

## PIECE-9 - Cell provisioning
realises: REQ-17, REQ-18, REQ-19, REQ-20, REQ-21, REQ-22
responsibility: creating cells in our cloud and in a customer's account, and installing and upgrading the managed services in them: the connection artifact, delegated and temporary access, isolation between accounts, and stopping every operation once access is revoked.

## PIECE-10 - Telemetry boundary
realises: REQ-20
responsibility: deciding what leaves a customer's cell (platform telemetry and usage counts with no content) and keeping the data and logs of agents and of every managed service out of our stores.

## PIECE-11 - Public documentation
realises: REQ-5
responsibility: the public guide that covers the whole path from a new account to agents answering over HTTPS and calling the managed services, and is the only script a dogfooding run follows.

## PIECE-12 - Service gateway
realises: REQ-23, REQ-24, REQ-27
responsibility: the single door to every managed service: checking the account key, serving the MCP endpoint, routing each call to its service, and returning errors in the shared shape.

## PIECE-13 - Memory
realises: REQ-13, REQ-25, REQ-27
responsibility: storing, recalling, forgetting and consolidating what an agent remembers.

## PIECE-14 - Document retrieval
realises: REQ-13, REQ-25, REQ-27
responsibility: ingesting documents and answering with citations.

## PIECE-15 - Context assembly
realises: REQ-25, REQ-27
responsibility: combining memory and documents into one context within a token budget.

## PIECE-16 - Prompt management
realises: REQ-25, REQ-27
responsibility: versioned prompts, labels and evaluations.

## PIECE-17 - Skills registry
realises: REQ-25, REQ-27
responsibility: the skills, bundles and revisions an agent runs.

## PIECE-18 - Conversation trust
realises: REQ-25, REQ-27
responsibility: observing conversations and enforcing policies on them.

## PIECE-19 - File storage
realises: REQ-13, REQ-25, REQ-27
responsibility: uploading, downloading, listing and sharing an agent's files.

## PIECE-20 - Traces
realises: REQ-25, REQ-26, REQ-27
responsibility: recording each execution with the managed-service calls it made and the cost of each.

## PIECE-21 - Platform security
realises: REQ-28, REQ-29, REQ-32
responsibility: scanning and signing every published image, keeping credentials out of every repository and revoking exposed ones, isolating platform components on the network, and the external penetration test.

## PIECE-22 - Operations
realises: REQ-30, REQ-31
responsibility: scheduled backups and restore drills, alerting and paging, runbooks, and the public status page.

## PIECE-23 - Environments as code
realises: REQ-33
responsibility: the versioned infrastructure code that creates the staging and production environments and everything they run on.
