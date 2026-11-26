// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Bundle fixture for k6 load tests.
//
// The API expects a git bundle. k6 cannot execute `git` at runtime, so
// we ship a pre-built bundle as a base64 constant embedded in this
// module. Regenerate with:
//
//   cd /tmp && rm -rf bundle && mkdir bundle && cd bundle && \
//     cat > package.json <<'EOF'
//     {"name":"loadtest","version":"1.0.0","main":"server.js","scripts":{"start":"node server.js"},"engines":{"node":">=18"}}
//     EOF
//     cat > package-lock.json <<'EOF'
//     {"name":"loadtest","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"loadtest","version":"1.0.0"}}}
//     EOF
//     cat > server.js <<'EOF'
//     const http = require("http");
//     http.createServer((_, r) => { r.writeHead(200); r.end('{"status":"ok"}'); }).listen(8080);
//     EOF
//     git init -q && git add -A && git -c user.email=k6@loopruntime.test -c user.name=k6 commit -q -m k6 && \
//     git bundle create /tmp/k6-loadtest.bundle HEAD && \
//     base64 -w0 /tmp/k6-loadtest.bundle
//
// Keeping the bundle in-script avoids the need to mount a volume and
// makes the k6 Job self-contained. The bundle is tiny (~1KB) so
// embedding it has no cost.
//
// The fixture is deliberately flat (no path: field in loopzero.yaml) to
// match Correction L from roadmap 01.

// Pre-generated bundle bytes. Replace this blob when regenerating.
// This placeholder will be overwritten at Job-startup time by an
// init container that generates a fresh bundle.
export const BUNDLE_BASE64_PLACEHOLDER = '';

/**
 * Load the bundle bytes. When running under the k6 operator with the
 * init container, the bundle file is mounted at /fixtures/bundle.bin
 * and read via k6's open() built-in. When running standalone, the
 * K6_BUNDLE_PATH env var can override.
 *
 * @returns {ArrayBuffer} raw bundle bytes
 */
export function loadBundleBytes() {
  const path = __ENV.K6_BUNDLE_PATH || '/fixtures/bundle.bin';
  // open() with 'b' returns an ArrayBuffer which http.file() can wrap.
  try {
    // eslint-disable-next-line no-undef
    return open(path, 'b');
  } catch (e) {
    throw new Error(`k6 bundle fixture not found at ${path}: ${e.message}. ` +
      `The Job init container should have created it. Check the k6-runner pod events.`);
  }
}
