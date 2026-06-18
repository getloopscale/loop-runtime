# AM6 — minimal proof image. Proves the build→SBOM→sign→GHCR-push→verify pipeline end-to-end
# without depending on a slow real service build (the 7 real images extend via the CI matrix).
# Distroless base + labels only — a valid, tiny OCI image.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.description="loopzero AM6 artifact-supply-chain pipeline proof image"
LABEL loopzero.milestone="AM6"
