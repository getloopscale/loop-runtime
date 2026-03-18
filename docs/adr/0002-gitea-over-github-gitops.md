# ADR 0002: Gitea as GitOps store instead of GitHub

## Status
Accepted

## Context
The system needs a Git repository to store desired state (Kubernetes manifests) that ArgoCD watches. Options: use the user's GitHub repo, or maintain a separate internal Git store.

## Decision
Use Gitea (self-hosted) as the GitOps store. User source code stays in their own GitHub/GitLab. LoopZero generates manifests and pushes to Gitea, which ArgoCD watches.

## Consequences
- Complete control over the GitOps repo lifecycle (create, archive, delete)
- No GitHub API rate limits on GitOps operations
- User never needs to grant write access to their source repo
- Additional infrastructure component to maintain (Gitea)
- Internal term "Gitea" must never appear in user-facing output
