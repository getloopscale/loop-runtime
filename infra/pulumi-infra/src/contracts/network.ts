// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { BaseLabels } from "../core/types";

export interface NetworkSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly cidr?: string;
  readonly tags: BaseLabels;
}

export interface NetworkOutput {
  readonly networkId: pulumi.Output<string>;
  readonly vpcId: pulumi.Output<string>;
}
