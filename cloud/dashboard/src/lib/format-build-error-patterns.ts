// format-build-error-patterns.ts
//
// 7 documented patterns + sanitization chain (a-e) for the
// formatBuildError helper. Plan
// 2026-06-08-dashboard-screens-review-fixes T3.1 + ADR D4 + EC-3 + EC-5
// absorptions.
//
// Wire-format contract: every match returns `{summary, detail}` where
// `summary` is the consumer-facing product-language message and
// `detail` is the operator-facing raw error. The dashboard renders
// `summary` by default and exposes `detail` behind a "Show details"
// toggle.
//
// CLAUDE.md Rule 3 (no internal terms in user-facing output):
//   The rendered `summary` MUST NOT contain any of:
//     ArgoCD | argocd | BuildKit | cosign | Kaniko | gyp | node-gyp
//   The fallback chain step (e) is the last line of defense — if any
//   of those tokens survives pattern matching, summary is replaced
//   with a generic "Build failed" message and the raw error moves to
//   `detail` for operator inspection.

export interface BuildErrorMessage {
  summary: string;
  detail: string;
}

interface ErrorPattern {
  id: string;
  test: RegExp;
  build: (input: string) => string;
}

const PATTERNS: readonly ErrorPattern[] = [
  {
    id: 'native-deps-python-missing',
    // Matches `gyp ERR!` lines emitted by node-gyp when Python is absent.
    test: /gyp ERR!/,
    build: () =>
      'A native dependency tried to compile from source but the build container is missing the Python compiler. Switch your dependency to a prebuilt-binary version OR use a runtime that ships binaries.',
  },
  {
    id: 'image-signing-did-not-run',
    test: /manifest_builder.*missing image digest/i,
    build: () =>
      'The container image was built but the signing step did not run. Re-trigger the deploy to retry signing.',
  },
  {
    id: 'source-bundle-not-uploaded',
    // MinIO/S3 NoSuchKey for the source bundle key produced by webhook
    // autodeploy. Usually means the upload step lost a race.
    test: /(NoSuchKey|Object does not exist).+source\//i,
    build: () =>
      "The source bundle was not found. This usually means the auto-deploy ran before the upload completed. Push again or click Redeploy.",
  },
  {
    id: 'npm-peer-dep-missing',
    test: /npm ERR! (peer dep|ERESOLVE)/i,
    build: () =>
      'A package in your project requires a peer dependency that is not installed. Add the peer dependency to package.json OR pin a compatible version range.',
  },
  {
    id: 'loopzero-yaml-missing-apps',
    test: /WorkflowSpec\.Plan must be non-nil.+at least one app/i,
    build: () =>
      'Your loopzero.yaml file has no apps defined. Add an `apps:` entry with at least one application before deploying.',
  },
  {
    id: 'source-clone-timeout',
    test: /clone-repo.*Max duration limit exceeded/i,
    build: () =>
      'Source clone took too long and timed out. This usually clears on retry. Click Redeploy to try again.',
  },
  {
    id: 'internal-credentials-rotating',
    // Catches our typed forbidden response from the auth gate.
    test: /forbidden.+service auth|service auth.+rejected/i,
    build: () =>
      'Internal credentials are being rotated. Retry in a few minutes.',
  },
];

export const PATTERN_IDS: readonly string[] = PATTERNS.map((p) => p.id);

// 10 KB byte cap (UTF-8 chars approx; we use string length as conservative
// proxy because JS string length is UTF-16 code units which over-counts
// for ASCII — safe upper bound). EC-5 absorption.
const MAX_INPUT_CHARS = 10 * 1024;

// Rule 3 internal terms blocklist. If ANY survives the strip chain,
// step (e) replaces the whole summary.
const FORBIDDEN_TERMS = /\b(ArgoCD|argocd|BuildKit|buildkit|cosign|Kaniko|kaniko|gyp|node-gyp)\b/;

const TRUNCATION_SUFFIX =
  ' … (truncated for display; see operator logs for full output)';

const GENERIC_FALLBACK =
  'Build failed — operator can see the full output via logs.';

/**
 * Format a raw build error string into a {summary, detail} pair.
 *
 * Algorithm:
 *   1. Empty input → "Unknown error." (defense in depth)
 *   2. Try each pattern in declaration order; first match wins
 *   3. No match → fallback chain (cap → strip workspace paths →
 *      strip stack frames → strip node:internal → blocklist sweep)
 *
 * `detail` is ALWAYS the raw input (for the "Show details" toggle).
 */
export function formatBuildError(raw: string): BuildErrorMessage {
  if (!raw) {
    return { summary: 'Unknown error.', detail: '' };
  }

  // Step 2: pattern match.
  for (const p of PATTERNS) {
    if (p.test.test(raw)) {
      return { summary: p.build(raw), detail: raw };
    }
  }

  // Step 3: fallback sanitization chain.
  return { summary: applyFallbackChain(raw), detail: raw };
}

/**
 * Sanitization chain (a-e) — exported for direct unit testing.
 *   (a) cap at MAX_INPUT_CHARS
 *   (b) strip /workspace/... paths
 *   (c) strip stack-frame fully-qualified file paths
 *   (d) strip node:internal/* runtime internals
 *   (e) blocklist sweep — if any forbidden term survives, replace
 *       summary with GENERIC_FALLBACK
 */
export function applyFallbackChain(raw: string): string {
  // (a) cap
  let truncated = false;
  let s = raw;
  if (s.length > MAX_INPUT_CHARS) {
    s = s.slice(0, MAX_INPUT_CHARS);
    truncated = true;
  }

  // (b) strip /workspace/<path>
  s = s.replace(/\/workspace\/[^\s)]+/g, '<path>');

  // (d) strip node:internal/* internals BEFORE (c) so the node:internal
  // prefix is consumed as a unit; otherwise step (c) eats the trailing
  // `/<path>.js:N` and leaves the bare `node:internal` token behind.
  s = s.replace(/node:internal\/[^\s)]+/g, '<runtime>');

  // (c) strip stack-frame file paths like /usr/local/.../foo.js:42 or :42:11
  s = s.replace(
    /\/[a-zA-Z0-9._\-/]+\.(js|mjs|cjs|ts|tsx|node|py|go):\d+(:\d+)?/g,
    '<frame>',
  );

  // (e) blocklist sweep — last line of defense for Rule 3
  if (FORBIDDEN_TERMS.test(s)) {
    return GENERIC_FALLBACK;
  }

  if (truncated) {
    s += TRUNCATION_SUFFIX;
  }
  return s;
}
