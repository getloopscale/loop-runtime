// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';
import type { AppName, AppSpec, ValidatorResult } from './types';

/**
 * Canonical loopzero.yaml frameworks. `custom` + `unknown` require a user-
 * provided Dockerfile in the app path.
 */
const KNOWN_FRAMEWORKS = [
  // Node.js ecosystem
  'nextjs', 'next', 'vite', 'remix', 'nestjs', 'express', 'node',
  'react', 'vue', 'svelte', 'astro', 'angular', 'fastify',
  // Go
  'gin', 'go',
  // Python
  'fastapi', 'django', 'python',
  // Ruby
  'sinatra', 'rails', 'ruby',
  // Java
  'spring', 'spring-boot', 'java',
  // PHP
  'laravel', 'slim', 'php',
  // Rust
  'axum', 'actix', 'rocket', 'rust',
  // Custom (user-provided Dockerfile)
  'custom',
];

/**
 * Orchestrator for the build-related validators: explicit build directive,
 * framework presence, Dockerfile presence for custom frameworks, and the
 * stray-Dockerfile auto-detect warning.
 */
export function validateAppBuild(
  name: AppName,
  app: AppSpec,
  projectRoot: string,
  skipFs: boolean,
): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  const warnings: string[] = [];

  const fw = app.framework ?? 'custom';

  validateExplicitDockerfileBuild(name, app, projectRoot, skipFs, errors);
  validateFrameworkKnown(name, fw, errors);
  validateCustomFrameworkDockerfile(name, app, fw, projectRoot, skipFs, errors);
  detectStrayDockerfileWarning(name, app, fw, projectRoot, skipFs, warnings);

  return { errors, warnings };
}

/**
 * Validate that an explicit `build: dockerfile` directive points at a
 * Dockerfile that actually exists.
 */
function validateExplicitDockerfileBuild(
  name: AppName,
  app: AppSpec,
  projectRoot: string,
  skipFs: boolean,
  errors: ValidatorResult['errors'],
): void {
  if (skipFs || app.build !== 'dockerfile') return;
  const dockerfileName = app.dockerfile ?? 'Dockerfile';
  const appPathResolved = path.resolve(projectRoot, app.path);
  const dockerfilePath = path.resolve(appPathResolved, dockerfileName);
  if (fs.existsSync(appPathResolved) && !fs.existsSync(dockerfilePath)) {
    errors.push({
      field: `apps.${name}.dockerfile`,
      message: `App '${name}': Dockerfile not found at '${app.path}/${dockerfileName}'`,
    });
  }
}

/**
 * Reject unknown frameworks BEFORE build (not a warning) — prevents
 * 5-minute build failures from unrecognized frameworks.
 */
function validateFrameworkKnown(
  name: AppName,
  fw: string,
  errors: ValidatorResult['errors'],
): void {
  if (fw === 'unknown' || KNOWN_FRAMEWORKS.includes(fw)) return;
  errors.push({
    field: `apps.${name}.framework`,
    message: `App '${name}': unknown framework '${fw}'. Supported: ${KNOWN_FRAMEWORKS.filter(f => f !== 'custom').join(', ')}. Use 'framework: custom' with a Dockerfile for unsupported frameworks.`,
  });
}

/**
 * Custom framework requires a Dockerfile in the app path.
 */
function validateCustomFrameworkDockerfile(
  name: AppName,
  app: AppSpec,
  fw: string,
  projectRoot: string,
  skipFs: boolean,
  errors: ValidatorResult['errors'],
): void {
  if (skipFs || (fw !== 'custom' && fw !== 'unknown')) return;
  const dockerfileName = app.dockerfile ?? 'Dockerfile';
  const appPathResolved = path.resolve(projectRoot, app.path);
  const dockerfilePath = path.resolve(appPathResolved, dockerfileName);
  if (fs.existsSync(appPathResolved) && !fs.existsSync(dockerfilePath)) {
    errors.push({
      field: `apps.${name}.dockerfile`,
      message: `App '${name}': framework 'custom' requires a Dockerfile at ${app.path}/${dockerfileName}. Add a Dockerfile or set a supported framework.`,
    });
  }
}

/**
 * Auto-detect: when a known-framework app has a stray Dockerfile in its
 * path but didn't ask for `build: dockerfile`, warn that the user may
 * have meant to set it.
 */
function detectStrayDockerfileWarning(
  name: AppName,
  app: AppSpec,
  fw: string,
  projectRoot: string,
  skipFs: boolean,
  warnings: string[],
): void {
  if (skipFs || app.build === 'dockerfile' || fw === 'custom' || fw === 'unknown') return;
  const appPathResolved = path.resolve(projectRoot, app.path);
  const dockerfilePath = path.resolve(appPathResolved, 'Dockerfile');
  if (fs.existsSync(dockerfilePath)) {
    warnings.push(`App '${name}': Dockerfile found at ${app.path}/Dockerfile but build is '${app.build ?? 'buildpack'}'. Set 'build: dockerfile' in loopzero.yaml to use your custom Dockerfile, or remove it to use the auto-generated one.`);
  }
}
