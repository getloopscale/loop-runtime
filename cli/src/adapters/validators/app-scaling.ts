// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { AppName, AppSpec, ValidatorResult } from './types';

/**
 * B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): cap that ANY plan must
 * structurally honor. Plan-specific caps (Starter=2 / Pro=5 / Team=10 /
 * Enterprise=NULL) are enforced server-side in BuildOrchestrator.SubmitBuild.
 */
const MAX_REPLICAS = 100;

const KNOWN_AUTOSCALING_FIELDS = new Set([
  'min', 'max', 'targetCPU', 'cooldown',
]);

/**
 * Validate scaling fields: `replicas` + `autoscaling.*` + their mutual
 * exclusion. Each sub-field is checked independently for type, range, and
 * structural validity.
 */
export function validateAppScaling(name: AppName, app: AppSpec): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  const warnings: string[] = [];

  const appType = app.type ?? 'server';
  const hasReplicas = app.replicas !== undefined && app.replicas !== null;
  const hasAutoscaling = app.autoscaling !== undefined && app.autoscaling !== null;

  validateReplicas(name, app, errors);
  if (hasAutoscaling) {
    validateAutoscaling(name, app, appType, errors, warnings);
  }

  // Mutual exclusion (MEET-059 Constraint 7).
  if (hasReplicas && hasAutoscaling) {
    errors.push({
      field: `apps.${name}`,
      message: `App '${name}': 'replicas' and 'autoscaling' are mutually exclusive — choose one. Use 'replicas' for a fixed count, or 'autoscaling' for HPA-driven scaling.`,
    });
  }

  return { errors, warnings };
}

function validateReplicas(name: AppName, app: AppSpec, errors: ValidatorResult['errors']): void {
  if (app.replicas === undefined || app.replicas === null) return;
  const r = app.replicas as number;
  if (typeof r !== 'number' || !Number.isInteger(r) || r < 1) {
    errors.push({
      field: `apps.${name}.replicas`,
      message: `App '${name}': replicas must be an integer >= 1 (got ${JSON.stringify(app.replicas)}).`,
    });
  } else if (r > MAX_REPLICAS) {
    errors.push({
      field: `apps.${name}.replicas`,
      message: `App '${name}': replicas ${r} exceeds maximum of ${MAX_REPLICAS} per service.`,
    });
  }
}

function validateAutoscaling(
  name: AppName,
  app: AppSpec,
  appType: string,
  errors: ValidatorResult['errors'],
  warnings: string[],
): void {
  const as = app.autoscaling!;
  validateAutoscalingMin(name, as.min, appType, errors);
  validateAutoscalingMax(name, as.max, errors);
  validateAutoscalingMinMaxRelation(name, as.min, as.max, errors);
  validateAutoscalingTargetCPU(name, as.targetCPU, errors);
  validateAutoscalingCooldown(name, as.cooldown, errors);
  detectUnknownAutoscalingFields(name, as, warnings);
}

function validateAutoscalingMin(
  name: AppName,
  min: number | undefined,
  appType: string,
  errors: ValidatorResult['errors'],
): void {
  if (min !== undefined && (typeof min !== 'number' || !Number.isInteger(min) || min < 0)) {
    errors.push({
      field: `apps.${name}.autoscaling.min`,
      message: `App '${name}': autoscaling.min must be an integer >= 0 (got ${JSON.stringify(min)}).`,
    });
  }
  // Scale-to-zero (min: 0) is not supported for workers — they don't
  // receive HTTP traffic, so there's no trigger to wake them up.
  if (min === 0 && appType === 'worker') {
    errors.push({
      field: `apps.${name}.autoscaling.min`,
      message: `App '${name}': autoscaling.min = 0 (scale-to-zero) is not supported for workers. Workers don't receive HTTP traffic for wake-up. Use min >= 1.`,
    });
  }
}

function validateAutoscalingMax(
  name: AppName,
  max: number | undefined,
  errors: ValidatorResult['errors'],
): void {
  if (max === undefined) return;
  if (typeof max !== 'number' || !Number.isInteger(max) || max < 1) {
    errors.push({
      field: `apps.${name}.autoscaling.max`,
      message: `App '${name}': autoscaling.max must be an integer >= 1 (got ${JSON.stringify(max)}).`,
    });
    return;
  }
  if (max > MAX_REPLICAS) {
    errors.push({
      field: `apps.${name}.autoscaling.max`,
      message: `App '${name}': autoscaling.max ${max} exceeds maximum of ${MAX_REPLICAS} per service.`,
    });
  }
}

function validateAutoscalingMinMaxRelation(
  name: AppName,
  min: number | undefined,
  max: number | undefined,
  errors: ValidatorResult['errors'],
): void {
  if (
    min !== undefined &&
    max !== undefined &&
    typeof min === 'number' &&
    typeof max === 'number' &&
    max < min
  ) {
    errors.push({
      field: `apps.${name}.autoscaling`,
      message: `App '${name}': autoscaling.max (${max}) must be >= autoscaling.min (${min}).`,
    });
  }
}

function validateAutoscalingTargetCPU(
  name: AppName,
  targetCPU: number | undefined,
  errors: ValidatorResult['errors'],
): void {
  if (targetCPU === undefined) return;
  if (typeof targetCPU !== 'number' || !Number.isInteger(targetCPU) || targetCPU < 1 || targetCPU > 100) {
    errors.push({
      field: `apps.${name}.autoscaling.targetCPU`,
      message: `App '${name}': autoscaling.targetCPU must be an integer between 1 and 100 (got ${JSON.stringify(targetCPU)}).`,
    });
  }
}

function validateAutoscalingCooldown(
  name: AppName,
  cooldown: number | undefined,
  errors: ValidatorResult['errors'],
): void {
  if (cooldown === undefined) return;
  if (typeof cooldown !== 'number' || !Number.isInteger(cooldown) || cooldown < 30 || cooldown > 3600) {
    errors.push({
      field: `apps.${name}.autoscaling.cooldown`,
      message: `App '${name}': autoscaling.cooldown must be an integer between 30 and 3600 seconds (got ${JSON.stringify(cooldown)}).`,
    });
  }
}

function detectUnknownAutoscalingFields(
  name: AppName,
  as: AppSpec['autoscaling'],
  warnings: string[],
): void {
  if (typeof as !== 'object' || as === null) return;
  for (const key of Object.keys(as)) {
    if (!KNOWN_AUTOSCALING_FIELDS.has(key)) {
      warnings.push(`Unknown field 'apps.${name}.autoscaling.${key}' -- ignored`);
    }
  }
}
