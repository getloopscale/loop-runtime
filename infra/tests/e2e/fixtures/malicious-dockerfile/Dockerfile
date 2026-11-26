# build-mode-dispatch-and-dockerfile-security-plan T4.4 fixture.
#
# This Dockerfile intentionally violates the FROM_REGISTRY_NOT_ALLOWED
# rule by using a base image from a registry NOT in LoopRuntime's allow-list.
# The dockerfile-lint workflow step must reject this BEFORE BuildKit
# is invoked.

FROM evil-registry.example.com/malicious:latest

CMD ["sh", "-c", "echo 'this should never run because lint must reject'"]
