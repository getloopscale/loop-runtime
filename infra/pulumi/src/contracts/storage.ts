import * as pulumi from "@pulumi/pulumi";
import { BaseLabels } from "../core/types";

export interface ObjectStorageSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly versioning?: boolean;
  readonly tags: BaseLabels;
}

export interface ObjectStorageOutput {
  readonly bucketName: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
  readonly region: pulumi.Output<string>;
}
