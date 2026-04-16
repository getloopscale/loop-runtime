# LoopZero Project Governance

This document describes the governance model for the LoopZero project.

## Principles

1. **Transparency** — All decisions are made in public channels (GitHub issues, PRs, discussions).
2. **Merit-based** — Contributions and demonstrated expertise determine roles, not affiliation.
3. **Vendor-neutral** — No single organization controls the project direction.
4. **Lazy consensus** — Proposals are accepted if no maintainer objects within 7 days.

## Roles

### Contributors

Anyone who contributes to the project — code, documentation, issues, reviews, or community support. No formal approval needed.

**Responsibilities:**
- Follow the [Code of Conduct](CODE_OF_CONDUCT.md)
- Follow the [Contributing Guide](CONTRIBUTING.md)
- Sign commits with DCO

### Reviewers

Experienced contributors who review pull requests in specific areas. Nominated by maintainers.

**Requirements:**
- Sustained contributions over 3+ months
- Demonstrated expertise in at least one component
- History of quality reviews

**Responsibilities:**
- Review PRs in their area of expertise
- Provide constructive, timely feedback
- Mentor new contributors

**Becoming a reviewer:** Nominated by a maintainer, approved by lazy consensus among maintainers.

### Maintainers

Core team members with write access and decision-making authority. See [MAINTAINERS.md](MAINTAINERS.md) for the current list.

**Requirements:**
- Sustained, significant contributions over 6+ months
- Deep understanding of project architecture and design principles
- History of quality reviews and mentorship
- Commitment to the project's long-term health

**Responsibilities:**
- Set technical direction
- Review and merge PRs
- Triage issues
- Manage releases
- Ensure project quality and security
- Mentor reviewers and contributors

**Becoming a maintainer:** Nominated by an existing maintainer, requires approval from 2/3 of current maintainers.

**Removing a maintainer:** Voluntary resignation (notify other maintainers) or removal for sustained inactivity (12+ months without contribution) or Code of Conduct violations. Removal requires 2/3 maintainer vote.

## Decision Making

### Day-to-Day Decisions

- **Lazy consensus** with a 7-day window.
- Any maintainer can propose a decision via GitHub issue or PR.
- If no maintainer objects within 7 days, the proposal is accepted.
- Silence is consent.

### Significant Decisions

Significant decisions require explicit approval:

- **Architecture changes** — Require at least 2 maintainer approvals and an ADR (Architecture Decision Record) in `docs/adr/`.
- **New dependencies** — Require 1 maintainer approval with justification against the dependency evaluation criteria in the project principles.
- **Breaking changes** — Require all active maintainer approvals and a migration path.
- **New maintainers/reviewers** — See role requirements above.

### Dispute Resolution

1. Discussion on the relevant GitHub issue/PR.
2. If unresolved, escalate to a maintainer vote (simple majority).
3. If still unresolved, escalate to the CNCF Technical Oversight Committee.

## Subprojects

| Subproject | Scope | Lead |
|------------|-------|------|
| `cli/` | CLI (TypeScript + Commander) | See MAINTAINERS.md |
| `api/` | API server (Go + chi + pgx) | See MAINTAINERS.md |
| `operators/` | Kubernetes operators (kubebuilder) | See MAINTAINERS.md |
| `pkg/` | Shared Go libraries | See MAINTAINERS.md |
| `infra/` | Infrastructure (Pulumi, Helm, k3d) | See MAINTAINERS.md |

Each subproject follows the same governance model. Subproject leads are maintainers with primary responsibility for that area.

## Releases

- Release process is documented in [RELEASES.md](RELEASES.md).
- Any maintainer can cut a release.
- Releases follow [Semantic Versioning](https://semver.org/).
- Changes are tracked in [CHANGELOG.md](CHANGELOG.md).

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). Violations can be reported to [conduct@cncf.io](mailto:conduct@cncf.io) or to the project maintainers.

## Amendments

This governance document can be amended by a 2/3 majority vote of active maintainers. Proposed changes must be open for comment for at least 14 days before voting.
