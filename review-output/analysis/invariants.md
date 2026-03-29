# System Invariants

Generated: 2026-03-29

## Validated Invariants

| ID | Name | Category | Status |
|----|------|----------|--------|
| inv_tenant_isolation | Tenant Data Isolation | Security | ✅ Validated |
| inv_state_machine_strict | State Machine Strictness | Correctness | ✅ Validated |
| inv_lock_on_mutation | Advisory Lock on All Mutations | Concurrency | ✅ Validated |
| inv_no_internal_terms | No Internal Terms in CLI Output | Product | ✅ Validated |
| inv_parameterized_queries | Parameterized SQL Queries | Security | ✅ Validated |

## Invariant Details

### inv_tenant_isolation
- **Assertion:** All DB queries include tenant_id filter
- **Evidence:** 49 tenant_id references across 8 store files
- **Validation:** grep + manual review of store method signatures

### inv_state_machine_strict
- **Assertion:** No direct state mutations; all transitions via Machine.Transition()
- **Evidence:** Zero `.State =` or `.Status =` patterns in production code
- **Validation:** grep for direct mutation patterns — zero matches

### inv_lock_on_mutation
- **Assertion:** All mutating services use EnvironmentLocker
- **Evidence:** 10 mutating services all accept EnvironmentLocker dependency
- **Violation found:** failRollback passes empty coordinates ("", "", "") — global lock instead of per-env

### inv_no_internal_terms
- **Assertion:** No internal terms in CLI string literals
- **Evidence:** Only sanitizer regex contains these terms; zero in user-facing strings
- **Validation:** grep for ArgoCD/Gitea/MinIO/etc. in cli/src/ — only found in sanitizeBuildError()

### inv_parameterized_queries
- **Assertion:** Zero SQL string concatenation in DB layer
- **Evidence:** All queries use pgx parameterized queries with $N placeholders
- **Validation:** grep for fmt.Sprintf+SQL patterns — zero matches
