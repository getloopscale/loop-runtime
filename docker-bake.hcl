// LoopZero Platform — Multi-image build definition.
// Usage: docker buildx bake --push
// CI: docker buildx bake --push --set "*.tags=${REGISTRY}/${name}:${TAG}"

variable "REGISTRY" {
  default = "registry.digitalocean.com/loopzero-system"
}

variable "TAG" {
  default = "dev"
}

variable "SHA" {
  default = ""
}

variable "SOURCE_DATE_EPOCH" {
  default = ""
}

group "default" {
  targets = ["api", "operator", "cloud", "dashboard", "harbor-init"]
}

target "api" {
  dockerfile = "api/Dockerfile"
  context    = "."
  tags = compact([
    "${REGISTRY}/loopzero-api:${TAG}",
    SHA != "" ? "${REGISTRY}/loopzero-api:${SHA}" : "",
  ])
  cache-from = ["type=registry,ref=${REGISTRY}/loopzero-api:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/loopzero-api:cache,mode=max"]
  platforms  = ["linux/amd64"]
  args = {
    SOURCE_DATE_EPOCH = "${SOURCE_DATE_EPOCH}"
  }
  output = ["type=registry"]
}

target "operator" {
  dockerfile = "operators/Dockerfile"
  context    = "."
  tags = compact([
    "${REGISTRY}/loopzero-tenant-controller:${TAG}",
    SHA != "" ? "${REGISTRY}/loopzero-tenant-controller:${SHA}" : "",
  ])
  cache-from = ["type=registry,ref=${REGISTRY}/loopzero-tenant-controller:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/loopzero-tenant-controller:cache,mode=max"]
  platforms  = ["linux/amd64"]
  args = {
    SOURCE_DATE_EPOCH = "${SOURCE_DATE_EPOCH}"
  }
  output = ["type=registry"]
}

target "cloud" {
  dockerfile = "cloud/Dockerfile"
  context    = "."
  tags = compact([
    "${REGISTRY}/loopzero-cloud:${TAG}",
    SHA != "" ? "${REGISTRY}/loopzero-cloud:${SHA}" : "",
  ])
  cache-from = ["type=registry,ref=${REGISTRY}/loopzero-cloud:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/loopzero-cloud:cache,mode=max"]
  platforms  = ["linux/amd64"]
  args = {
    SOURCE_DATE_EPOCH = "${SOURCE_DATE_EPOCH}"
  }
  output = ["type=registry"]
}

target "dashboard" {
  dockerfile = "cloud/dashboard/Dockerfile"
  context    = "cloud/dashboard"
  tags = compact([
    "${REGISTRY}/loopzero-dashboard:${TAG}",
    SHA != "" ? "${REGISTRY}/loopzero-dashboard:${SHA}" : "",
  ])
  cache-from = ["type=registry,ref=${REGISTRY}/loopzero-dashboard:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/loopzero-dashboard:cache,mode=max"]
  platforms  = ["linux/amd64"]
  args = {
    SOURCE_DATE_EPOCH = "${SOURCE_DATE_EPOCH}"
  }
  output = ["type=registry"]
}

target "harbor-init" {
  dockerfile = "infra/scripts/harbor-init/Dockerfile"
  context    = "."
  tags = compact([
    "${REGISTRY}/harbor-init:${TAG}",
    SHA != "" ? "${REGISTRY}/harbor-init:${SHA}" : "",
  ])
  cache-from = ["type=registry,ref=${REGISTRY}/harbor-init:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/harbor-init:cache,mode=max"]
  platforms  = ["linux/amd64"]
  args = {
    SOURCE_DATE_EPOCH = "${SOURCE_DATE_EPOCH}"
  }
  output = ["type=registry"]
}
