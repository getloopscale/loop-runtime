# Technical requirements: loopzero

Agreed in the brainstorm session of 2026-10-04 and 2026-10-05
(`.squad/records/brainstorms/2026-10-04-session.md`). Phase 3 of 4; not yet signed.

Each requirement states what a user or an operator can observe, not how it is built. The
mechanism behind the BYOC requirements is decided elsewhere (loopzero#490, option B, and the
Cluster API proposal). "A run" means one dogfooding run as defined in `objectives.md`. Every time
limit and count marked as a hypothesis is unmeasured and gets revisited with the objectives.

## REQ-1 - A new team creates its account from the CLI, with no action on our side
serves: OBJ-1
statement: a person with a new email reaches a logged-in `loopzero` session without anyone at LoopZero approving, provisioning or answering anything.
acceptance: each run goes from a new email to `loopzero login` with no human action on our side, and the audit log shows no admin action between account creation and the first deploy.

## REQ-2 - A LoopZeroKit project is recognized with no manual configuration
serves: OBJ-1
statement: `loopzero init` in a LoopZeroKit project finds every agent and service on its own; the user edits no file to deploy.
acceptance: on the sample project (at least two agents) `loopzero init` then `loopzero deploy` succeed, and after `init` the `git status` output shows only files `init` generated and no user edit.

## REQ-3 - Every exposed agent gets an HTTPS URL with no infrastructure step
serves: OBJ-1
statement: after a deploy, every agent the project exposes answers on its own HTTPS URL, and the user ran no command outside the `loopzero` CLI to get there.
acceptance: after deploy every exposed agent answers 2xx over HTTPS with the certificate verified, and the run log holds no command outside the `loopzero` CLI.

## REQ-4 - Every failure on the first-deploy path says what went wrong and what to do next
serves: OBJ-1, OBJ-2
statement: a failure while logging in, initializing or deploying names its cause and the next action in product language and exits with a contract code from 0 to 5.
acceptance: an automated test injects a failure at each first-deploy step (wrong credentials, invalid project, broken build, quota exceeded, network down) and asserts an exit code in 0..5, no internal term forbidden by Rule 3, and a stated next action. The five failures are a hypothesis.

## REQ-5 - The public guide covers the whole first-deploy path
serves: OBJ-1
statement: the public documentation is enough to go from a new account to agents answering over HTTPS and calling the managed services, and the runs use nothing else.
acceptance: every step taken and every managed-service call made in a run maps to a section of the public guide; a step outside the guide fails the run and becomes a backlog item.

## REQ-6 - Rollback serves the previous release again, with no rebuild
serves: OBJ-2
statement: `loopzero rollback` puts the previous, already built release back in service without building anything.
acceptance: in each run, after deploying v1 then v2, `loopzero rollback` makes the URL answer with v1's marker, the build history shows no build started between the command and that answer, and the rollback time is recorded.

## REQ-7 - A failing release aborts on its own while the serving release keeps serving
serves: OBJ-2
statement: a release that fails its health check or its progressive analysis is aborted with no intervention, and the release already serving keeps answering throughout.
acceptance: each run deploys a release whose health check fails on purpose; a probe hits the URL every second for the whole deploy and every request gets 2xx from the previous release; the broken release ends aborted. This covers a failed health check only: a release that passes health and misbehaves serves the canary share until analysis aborts it.

## REQ-8 - Concurrent deploys to one environment end consistent
serves: OBJ-2
statement: when two deploys target the same environment at once, one proceeds and the other waits or is refused with a clear message, and no release is left half applied.
acceptance: an automated test starts two deploys to one environment at once; exactly one proceeds, the other waits or is refused with a contract exit code and a clear message, and at the end exactly one release is active and none sits in an intermediate state.

## REQ-9 - Every release state change carries a time
serves: OBJ-2
statement: each transition of a release is recorded with the time it happened, so rollback time is measurable in production.
acceptance: in production every release in a terminal state has a time for each transition, and a query returns the period's rollback p95.

## REQ-10 - A pull request gets a preview of the whole project, removed when it closes
serves: OBJ-2
statement: opening a pull request creates a preview with every agent of the project, and closing it removes the preview.
acceptance: each run opens a pull request and the preview answers over HTTPS with every agent before the run ends; after the pull request closes the preview stops answering within 10 minutes (hypothesis).

## REQ-11 - The team sets and reads its spending ceiling from the CLI
serves: OBJ-3
statement: the team sets, changes and reads its spending ceiling with the `loopzero` CLI.
acceptance: each run sets, reads, changes and re-reads the ceiling from the CLI, both reads match the values set, and no step uses the dashboard; an automated test sends an invalid value (negative, empty) and asserts exit code 1 with a clear message.

## REQ-12 - An idle service goes to zero and wakes on the first request
serves: OBJ-3
statement: a service with no traffic for a period goes to zero replicas on its own, and the first request after that wakes it.
acceptance: after a deploy and 15 minutes with no traffic the service is at zero replicas, and the first request then gets 2xx within 30 seconds (both numbers are hypotheses).

## REQ-13 - Usage is metered per service and per managed-service operation
serves: OBJ-3
statement: usage is metered per deployed service and per managed-service operation (memory writes and reads, documents ingested and retrieved, stored files, traces), and time a deployed service spends at zero replicas adds nothing to compute usage.
acceptance: over a window the service spent at zero replicas, the metered compute-hours query for it returns 0 (the same query as OBJ-3's production metric); in a run, the metered counts of managed-service operations match exactly the calls the agent recorded in its own log.

## REQ-14 - The team is alerted at 80% of its ceiling
serves: OBJ-3
statement: when usage reaches 80% of the ceiling the team receives an alert before usage reaches 100%.
acceptance: each run sets a low ceiling and forces usage past it; the alert's arrival time is earlier than the time usage crosses 100%, and both times are recorded.

## REQ-15 - Billable usage stops at the ceiling
serves: OBJ-3
statement: when usage reaches the ceiling, services go to zero, new deploys are refused with a contract exit code and a clear message, and billable managed-service calls are refused with a clear message; raising the ceiling brings them back.
acceptance: after usage crosses 100% the services reach zero within 5 minutes (hypothesis), a new deploy is refused with exit code 3 and a message naming the ceiling, a billable memory write is refused with a message naming the ceiling, and the simulated invoice is at or below the ceiling; raising the ceiling brings the services back and the next deploy succeeds.

## REQ-16 - Usage and the projected invoice are visible at any time
serves: OBJ-3
statement: the team sees the period's usage and the projected invoice from the CLI at any time; under BYOC the invoice states that it covers only LoopZero's charges and not compute.
acceptance: the CLI's projection during a run is within 10% of the final simulated invoice (hypothesis), and a BYOC run's invoice has no compute line.

## REQ-17 - A customer connects a cloud account by running one artifact
serves: OBJ-4
statement: the customer connects an account by running a single artifact we provide, with no manual cluster or network step.
acceptance: a run in the member account acting as customer shows exactly one artifact run on the customer side and no command outside `loopzero` and that artifact, and 60 minutes or less pass from running it to the first agent answering over HTTPS.

## REQ-18 - The same commands work in the customer's account
serves: OBJ-4, OBJ-5
statement: the same project and commands (deploy, rollback, preview per pull request, HTTPS URL) work in the customer's account, the managed services answer from the customer's cell, and the team picks the target per environment.
acceptance: the OBJ-1, OBJ-2 and OBJ-5 verification scripts run unchanged against the BYOC target, only the target differs, and all pass; network flows show the managed-service calls never leave the customer's account.

## REQ-19 - We hold no long-lived customer credential
serves: OBJ-4
statement: access to a customer account is delegated and temporary and bound to a per-account identifier that stops another customer from using the same path; we store no long-lived credential.
acceptance: a scan of our secret stores and database finds only the access reference and the identifier per customer account and zero access keys, and a test using the right reference with a wrong identifier is refused.

## REQ-20 - Only platform telemetry leaves the customer's account
serves: OBJ-4
statement: agent data and logs stay in the customer's account, including what the managed services store; only platform telemetry and usage counts with no content leave it.
acceptance: during a run every egress flow from the customer cell goes only to the platform telemetry allowlist, and a scan of our stores finds no log line, memory, document, file, prompt, conversation or trace content of that customer.

## REQ-21 - Customer accounts are isolated from each other
serves: OBJ-4
statement: only one customer's workloads run in that customer's account, and nothing there reaches another customer's data or workloads, nor the reverse.
acceptance: with two customer accounts A and B, a workload in A trying to reach B's endpoints and data is refused, the reverse is refused too, and A's inventory lists only A's workloads.

## REQ-22 - The customer revokes our access by undoing the artifact
serves: OBJ-4
statement: undoing the artifact revokes our access; we stop every operation on that account, and the next command that needs it fails with a clear message and a contract exit code.
acceptance: after the artifact is undone, within 5 minutes (hypothesis) the next `loopzero deploy` to that target fails with exit code 4 and a message naming the revoked access, and our audit log shows no successful operation on that account after the revocation.

## REQ-23 - One key per account opens every managed service
serves: OBJ-5
statement: a single key per account authenticates every managed service, from the agent's code and through the MCP endpoint; there is no key or sign-up per service.
acceptance: in each run the agents call all eight managed services with the account's key only, and an automated test calls each service with no key and with an invalid key and every call is refused with a clear message.

## REQ-24 - A deployed agent reaches the services with nothing to provision
serves: OBJ-5, OBJ-1
statement: an agent deployed with `loopzero deploy` reaches the managed services with no extra command to create or configure them.
acceptance: the run log holds no command between `loopzero deploy` and the first managed-service call, the agent reads the endpoint and key from the environment the deploy provides, and the project's repository holds no key.

## REQ-25 - No team reaches another team's data in any service
serves: OBJ-5
statement: in every managed service, one team's key never reads or writes another team's data.
acceptance: an automated test run against each of the eight services creates teams A and B; every attempt by A's key to read or write B's resources is refused and no response carries B's data, and the reverse holds.

## REQ-26 - Every execution appears in Lens with its service calls and cost
serves: OBJ-5
statement: each execution of an agent appears in Lens as a trace holding the managed-service calls it made and the cost of each.
acceptance: for one execution of the sample agent, Lens shows a trace with one span per managed-service call, each with its cost, and the number of spans equals the number of calls the agent recorded in its own log.

## REQ-27 - Every service's failures are measurable per call
serves: OBJ-5
statement: each managed service records its failures per call, so the month's 5xx rate per service can be queried, and its errors use one shared shape in product language.
acceptance: a test takes down each service's dependency and makes one call; the month's 5xx query counts that failure, and the response body carries `code` and `message` and no term forbidden by Rule 3.
