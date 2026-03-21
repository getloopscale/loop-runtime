import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // DO Spaces don't support tags — tracked via naming convention only.
    const bucket = new digitalocean.SpacesBucket(spec.name, {
      name: spec.name,
      region: spec.region,
      versioning: {
        enabled: spec.versioning ?? false,
      },
    });

    return {
      bucketName: bucket.name,
      endpoint: bucket.bucketDomainName,
      region: pulumi.output(spec.region),
    };
  }
}
