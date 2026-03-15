# Sprint 2 — Manual Testing Guide

> **Audience:** Anyone — no Kubernetes or Go experience required.
> **Time:** ~45 minutes (infra already running) or ~60 minutes (from scratch).
> **Rule:** Every test uses the `loopzero` CLI exactly as a real user would. No `kubectl`, no `psql`, no port-forwards during the tests themselves. Infrastructure setup is a one-time prerequisite.

---

## Test Checklist

Mark each test as you complete it. Every test has an exact command and exact expected output.

| # | What you test | Command | Expected | ✓ |
|---|--------------|---------|----------|---|
| 1 | Login | `loopzero login --token <PAT>` | `Authenticated as <email> (team: acme)`, exit 0 | [ ] |
| 2 | Who am I | `loopzero whoami` | Shows email + team, exit 0 | [ ] |
| 3 | Logout | `loopzero logout` then `loopzero whoami` | Logout exit 0, whoami exit 4 | [ ] |
| 4 | Init simple app | `loopzero init --yes --name my-app` | `Generated loopzero.yaml with 1 app`, exit 0 | [ ] |
| 5 | Init blocked (exists) | `loopzero init --yes --name my-app` | `loopzero.yaml already exists`, exit 1 | [ ] |
| 6 | Init force overwrite | `loopzero init --yes --name my-app --force` | Overwrites, exit 0 | [ ] |
| 7 | Init without login | logout → `loopzero init` | Exit 4 | [ ] |
| 8 | **Deploy simple app** | `loopzero init` + `loopzero deploy` | Real build + image + deploy, URL shown | [ ] |
| 9 | Deploy JSON | `loopzero deploy --json` | JSON with app, correct URL | [ ] |
| 10 | Deploy no-wait | `loopzero deploy --no-wait` | `Deploy started (build-xxx)`, exit 0 | [ ] |
| 11 | Deploy to staging | `loopzero deploy --env staging` | Build + deploy to staging env | [ ] |
| 12 | Deploy UPPERCASE env | `loopzero deploy --env PRODUCTION` | `Environment names must be lowercase.`, exit 1 | [ ] |
| 13 | Deploy without git | Run outside a git repo | `Run from inside a git repository.`, exit 1 | [ ] |
| 14 | Deploy without loopzero.yaml | Run in empty git repo | `No loopzero.yaml found. Run: loopzero init`, exit 1 | [ ] |
| 15 | Deploy without login | logout → `loopzero deploy` | Exit 4 | [ ] |
| 16 | Deploy unknown app | `loopzero deploy --app fake` | Exit 1 before upload | [ ] |
| 17 | Concurrent deploy | Two `loopzero deploy` at once | One 202, one rejected (exit 3) | [ ] |

---

## Before You Start (One-Time Setup)

These steps set up the development infrastructure. You only need to do this once. If someone already set it up, skip to **"Start Testing"**.

### Step 1: Install tools

You need these installed on your machine:

| Tool | Minimum version | Install |
|------|----------------|---------|
| Docker | 24+ | https://docs.docker.com/get-docker/ |
| Node.js | 20+ | https://nodejs.org/ |
| Go | 1.22+ | https://go.dev/dl/ |
| k3d | 5+ | `curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh \| bash` |
| Tilt | 0.33+ | `curl -fsSL https://raw.githubusercontent.com/tilt-dev/tilt/master/scripts/install.sh \| bash` |
| Helm | 3+ | https://helm.sh/docs/intro/install/ |
| Task | 3+ | https://taskfile.dev/installation/ |

Verify everything:

```bash
docker --version
node --version
go version
k3d version
tilt version
helm version --short
task --version
```

### Step 2: Increase system limits

k3d runs multiple Kubernetes clusters. Linux needs higher file watcher limits:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=1024
```

To make it permanent (survives reboot):

```bash
echo "fs.inotify.max_user_watches=524288" | sudo tee -a /etc/sysctl.d/99-loopzero.conf
echo "fs.inotify.max_user_instances=1024" | sudo tee -a /etc/sysctl.d/99-loopzero.conf
```

### Step 3: Get a GitHub token

You need a GitHub Personal Access Token (PAT) with scopes `read:user` and `user:email`.

1. Go to https://github.com/settings/tokens
2. Click "Generate new token (classic)"
3. Select scopes: `read:user`, `user:email`
4. Copy the token (starts with `ghp_`)

### Step 4: Boot the infrastructure

Open a terminal in the project root:

```bash
cd /home/paulo/Projetos/loopzero/loopzero

# Create clusters + install everything (10-15 min first time)
task dev:up
```

Wait until you see: `Clusters and dependencies are ready. Run 'tilt up' to start the dev loop.`

### Step 5: Apply the WorkflowTemplate

The build pipeline template needs to be applied to the build cluster:

```bash
kubectl --context k3d-loopzero-build -n argo apply -f infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml
```

> This step is needed after cluster creation or when the template changes. You should see: `workflowtemplate.argoproj.io/loopzero-build-nodejs-v1 configured`.

### Step 6: Start the dev server

In the **same terminal** (it stays in foreground):

```bash
tilt up
```

Wait until the Tilt dashboard (http://localhost:10350) shows all resources green. This builds the API server and deploys it to the local cluster.

### Step 7: Open a new terminal for testing

Open a **second terminal**. All test commands run here.

```bash
cd /home/paulo/Projetos/loopzero/loopzero
```

### Step 8: Build the CLI

```bash
cd cli && npm install && npm run build && cd ..
```

### Step 9: Create the `loopzero` command

```bash
cat > /tmp/loopzero << 'SCRIPT'
#!/bin/bash
export LOOPZERO_API_URL="${LOOPZERO_API_URL:-http://localhost:3001}"
node /home/paulo/Projetos/loopzero/loopzero/cli/dist/index.js "$@"
SCRIPT
chmod +x /tmp/loopzero
export PATH="/tmp:$PATH"
```

Verify it works:

```bash
loopzero --version
# Should print: 0.1.0
```

### Step 10: Seed the test user

```bash
task dev:seed
```

This creates a user and team (`acme`) in the database, linked to your GitHub account.

> **If `task dev:seed` fails** (port conflict), run it manually — see Troubleshooting at the bottom.

---

## Start Testing

**From this point on, you only use the `loopzero` command.** No kubectl, no psql, no curl. You are a user.

Save your GitHub token:

```bash
export MY_TOKEN="ghp_your_token_here"
```

---

### Test 1: Login

```bash
loopzero login --token $MY_TOKEN
echo "Exit code: $?"
```

**You should see:**
```
Authenticated as your@email.com (team: acme)
Exit code: 0
```

---

### Test 2: Who am I

```bash
loopzero whoami
echo "Exit code: $?"
```

**You should see:**
```
Authenticated as: your@email.com
Team: acme
Exit code: 0
```

---

### Test 3: Logout and verify

```bash
loopzero logout
echo "Exit code: $?"
```

**You should see:** `Logged out`, exit code 0.

Now check that you're logged out:

```bash
loopzero whoami
echo "Exit code: $?"
```

**You should see:** `Not authenticated. Run: loopzero login`, exit code **4**.

Log back in for the next tests:

```bash
loopzero login --token $MY_TOKEN
```

---

### Test 4: Create a project and initialize

Create a simple Node.js app:

```bash
mkdir -p /tmp/my-app && cd /tmp/my-app

cat > package.json << 'EOF'
{ "name": "my-app", "version": "1.0.0", "scripts": { "start": "node server.js" } }
EOF

cat > server.js << 'EOF'
const http = require('http');
http.createServer((req, res) => {
  if (req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok' }));
    return;
  }
  res.writeHead(200);
  res.end('hello from loopzero');
}).listen(process.env.PORT || 3000, () => {
  console.log('Listening on port ' + (process.env.PORT || 3000));
});
EOF

npm install --package-lock-only
git init && git add -A && git commit -m "my first app"
```

> **Important:** The app includes a `/health` endpoint and a `package-lock.json`. Both are required for the build pipeline to work correctly.

Now initialize loopzero:

```bash
loopzero init --yes --name my-app
echo "Exit code: $?"
```

**You should see:**
```
Generated loopzero.yaml with 1 app.
Review and edit if needed, then run: loopzero deploy
Exit code: 0
```

Check the generated file:

```bash
cat loopzero.yaml
```

**You should see** something like:
```yaml
project: my-app

apps:
  my-app:
    path: .
    framework: node
    port: 3000
```

---

### Test 5: Init blocks when loopzero.yaml exists

```bash
loopzero init --yes --name my-app
echo "Exit code: $?"
```

**You should see:** `loopzero.yaml already exists. Use --force to overwrite.`, exit code **1**.

---

### Test 6: Init with --force overwrites

```bash
loopzero init --yes --name my-app --force
echo "Exit code: $?"
```

**You should see:** `Generated loopzero.yaml with 1 app`, exit code 0.

---

### Test 7: Init without login

```bash
loopzero logout
loopzero init --yes --name test --force
echo "Exit code: $?"
```

**You should see:** exit code **4** (authentication required).

Log back in:

```bash
loopzero login --token $MY_TOKEN
```

---

### Test 8: Deploy a real app (the main test)

This is the Sprint 2 exit gate test. The deploy runs the **real pipeline**: source upload → Argo Workflow → Kaniko image build → K8s manifest generation → GitOps push → ArgoCD sync → health probe → URL returned.

> **Expected time:** 2-4 minutes for the full pipeline. The CLI shows real-time progress events.

```bash
cd /tmp/my-app
git add -A && git commit -m "add loopzero config" --allow-empty
loopzero deploy
echo "Exit code: $?"
```

**You should see progress events like:**
```
Building ...
✓ Build succeeded
Deploying ...
Deploy completed in 150s
  my-app: http://my-app.acme.loopzero.local
Exit code: 0
```

**What to verify:**
- Real-time progress: you see build stages as they happen
- **URL format:** `http://{project}.{tenant}.loopzero.local` (local dev domain)
- Exit code is **0**
- Total time ~2-4 minutes (real Kaniko build, not simulated)

> **If you see `Build failed:`** — check the error message. Common causes: missing `package-lock.json` (run `npm install` first), missing `start` script in package.json.

> **If you see `Deploy failed:`** — the build succeeded but health probe or ArgoCD sync failed. This is acceptable for the Sprint 2 exit gate as long as the build pipeline completes.

---

### Test 9: Deploy with JSON output

Wait for the previous deploy to finish, then:

```bash
loopzero deploy --json
echo "Exit code: $?"
```

**You should see** structured JSON:
```json
{
  "deploy_id": "d-abcd",
  "status": "success",
  "duration_seconds": 150,
  "apps": [
    {
      "name": "my-app",
      "status": "deployed",
      "url": "http://my-app.acme.loopzero.local",
      "build_duration_seconds": 150
    }
  ]
}
```

**Verify:**
- `status` is `"success"`
- `deploy_id` starts with `d-` followed by 4 hex characters (like `d-8f3a`)
- App has a real URL (not empty, not null)
- `build_duration_seconds` is a real number (not 0)
- Exit code is 0

---

### Test 10: Deploy with --no-wait

```bash
loopzero deploy --no-wait
echo "Exit code: $?"
```

**You should see:**
```
Deploy started (build-xxxxxxxx). Wait for it to complete, or try again later.
Exit code: 0
```

This returns immediately without waiting for the build to finish. Exit code is always 0 if the submit was accepted.

---

### Test 11: Deploy to a different environment

```bash
loopzero deploy --env staging
echo "Exit code: $?"
```

**You should see:** progress events followed by `Deploy completed`, exit code 0. The deploy targets `staging` instead of the default `production`. The URL will be `http://my-app.acme.loopzero.local` (different namespace in the cluster).

---

### Test 12: Deploy with invalid environment name

```bash
loopzero deploy --env PRODUCTION
echo "Exit code: $?"
```

**You should see:** `Environment names must be lowercase.`, exit code **1**. This fails **instantly** — no upload, no build.

---

### Test 13: Deploy outside a git repo

```bash
mkdir -p /tmp/not-a-repo && cd /tmp/not-a-repo
echo "project: x" > loopzero.yaml
loopzero deploy
echo "Exit code: $?"
cd /tmp/my-app
```

**You should see:** `Run from inside a git repository.`, exit code **1**.

---

### Test 14: Deploy without loopzero.yaml

```bash
mkdir -p /tmp/empty-repo && cd /tmp/empty-repo && git init 2>/dev/null
loopzero deploy
echo "Exit code: $?"
cd /tmp/my-app
```

**You should see:** `No loopzero.yaml found. Run: loopzero init`, exit code **1**.

---

### Test 15: Deploy without login

```bash
loopzero logout
loopzero deploy
echo "Exit code: $?"
```

**You should see:** `Not authenticated. Run: loopzero login`, exit code **4**.

Log back in:

```bash
loopzero login --token $MY_TOKEN
```

---

### Test 16: Deploy unknown app

```bash
cd /tmp/my-app
loopzero deploy --app nonexistent
echo "Exit code: $?"
```

**You should see:** an error mentioning `nonexistent` and listing available apps, exit code **1**. This fails **before** uploading any code (fail-fast behavior).

---

### Test 17: Concurrent deploy rejection

Open **two terminals** in `/tmp/my-app`. Run the deploy in both simultaneously:

**Terminal A:**
```bash
loopzero deploy --no-wait
echo "Exit A: $?"
```

**Terminal B** (immediately after A):
```bash
loopzero deploy --no-wait
echo "Exit B: $?"
```

**Expected:** One returns exit 0 (accepted), the other returns exit **3** with message `A deploy is already in progress for this environment. Wait for it to complete, or try again later.`

This validates that the environment lock prevents concurrent deploys.

---

## After Testing

### Clean up test directories

```bash
rm -rf /tmp/my-app /tmp/not-a-repo /tmp/empty-repo
```

### Stop the dev server

In the Tilt terminal, press `Ctrl+C`.

### (Optional) Delete clusters

```bash
cd /home/paulo/Projetos/loopzero/loopzero
task clusters:delete
```

---

## Troubleshooting

### `loopzero: command not found`

Run Step 9 again to create the `/tmp/loopzero` wrapper. Or make sure `/tmp` is in your PATH:

```bash
export PATH="/tmp:$PATH"
```

### `loopzero deploy` shows `Build failed: ...`

The build pipeline runs real steps. Common causes:

| Error | Cause | Fix |
|-------|-------|-----|
| `No lockfile found` | Missing package-lock.json | Run `npm install` in your project, commit the lockfile |
| `Dockerfile not found` | (Should not happen — Dockerfile is auto-generated) | Check workflow logs in Argo UI |
| `npm ci failed` | Corrupted or outdated lockfile | Delete `package-lock.json`, run `npm install`, commit |

### `loopzero deploy` shows `Deploy failed: ...`

The build succeeded but deployment failed. Common causes:

| Error | Cause | Fix |
|-------|-------|-----|
| `HEALTH_PROBE_FAILED` | App doesn't respond on `/health` | Add a `/health` endpoint to your app |
| `sync failed` | ArgoCD can't apply manifests | Check for duplicate resources — try a fresh project name |

### `loopzero deploy` hangs (no output after "Building ...")

The CLI waits up to 5 minutes. If it hangs:
1. Check the Tilt dashboard (http://localhost:10350) — is the API green?
2. Press `Ctrl+C` and retry. The build may still be running in the background.
3. If the same build is still running, wait for it to finish or cancel via a second deploy attempt (which will be rejected with 409).

### `Not authenticated` on every command

Your token may have expired or the credentials file was deleted. Run:

```bash
loopzero login --token $MY_TOKEN
```

### `Connection refused` errors

The API server may not be running or the port-forward is stale. Check the Tilt dashboard at http://localhost:10350. If the `loopzero-api` resource is red, click it to see logs.

### `task dev:seed` fails

If the seed task fails due to port conflicts, seed manually:

```bash
kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql 15433:5432 &
sleep 2
PGPASSWORD=loopzero-dev-password psql -h localhost -p 15433 -U loopzero -d loopzero <<'SQL'
INSERT INTO users (id, email, github_id, display_name)
VALUES ('github-YOUR_GITHUB_USERNAME', 'YOUR_EMAIL', YOUR_GITHUB_ID, 'YOUR_NAME')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenants (id, display_name, plan, owner_id)
VALUES ('acme', 'acme', 'starter', 'github-YOUR_GITHUB_USERNAME')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('acme', 'github-YOUR_GITHUB_USERNAME', 'owner')
ON CONFLICT (tenant_id, user_id) DO NOTHING;
SQL
kill %1 2>/dev/null
```

Replace `YOUR_GITHUB_USERNAME`, `YOUR_EMAIL`, and `YOUR_GITHUB_ID` with your actual values. Find your GitHub ID at `https://api.github.com/users/YOUR_USERNAME`.

### `Environment names must be lowercase` on a valid name

Environment names must match `^[a-z][a-z0-9-]{0,62}$`: start with a lowercase letter, only lowercase letters, numbers, and hyphens, max 63 characters.

---

## Running Without Tilt

If Tilt is not running, the API is not accessible via localhost:3001. You need to set up port-forwarding manually:

```bash
kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/loopzero-api 3001:3001 &
```

Alternatively, run `tilt up` to start the full dev loop with automatic port-forwarding.

> **Tip:** If you lose the port-forward (e.g., after a laptop sleep), just re-run the command above. The `&` runs it in the background so your terminal stays usable.

---

## What the pipeline does (real, not simulated)

Sprint 2 runs a **real build pipeline** end-to-end:

| Step | What happens | Where |
|------|-------------|-------|
| Source upload | CLI creates git bundle, API pushes to Gitea | mgmt cluster |
| clone-repo | Argo Workflow clones from Gitea source repo | build cluster |
| validate-workspace | Checks package.json + lockfile exist (fast-fail) | build cluster |
| install-deps | `npm ci` (or yarn/pnpm) with retry + cache | build cluster |
| build-apps | `npm run build` | build cluster |
| generate-dockerfile | Auto-generates Dockerfile if user didn't provide one | build cluster |
| build-image | Kaniko builds Docker image, pushes to registry | build cluster |
| generate-manifests | Creates Deployment + Service + IngressRoute YAML | build cluster |
| gitops-push | Commits manifests to Gitea GitOps repo | build cluster |
| notify-api | Callback to API with build status + image digest + URL | build cluster → mgmt |
| ArgoCD sync | Syncs manifests from GitOps repo to workload cluster | mgmt → workload |
| Health probe | HTTP GET to app's `/health` endpoint via Ingress | mgmt → workload |

## Known limitations (Sprint 2)

These are intentional scope boundaries, not bugs:

| What | Why | When |
|------|-----|------|
| `loopzero logs`, `loopzero status`, `loopzero rollback` | Not implemented yet | Sprint 3-4 |
| HTTPS URLs | Local dev uses HTTP via Traefik | Sprint 6 (cert-manager) |
| Multi-app monorepo deploy | Pipeline builds one image per deploy (Sprint 2 scope) | Sprint 4+ |
| Custom Dockerfile | Auto-generated Dockerfile covers Node.js; user Dockerfile is respected if present | Sprint 3 (Python, Go) |
| Preview environments (PR-based) | Requires webhook integration | Sprint 5 |

---

## Exit Codes Reference

| Code | Meaning | Example |
|------|---------|---------|
| 0 | Success | Deploy completed |
| 1 | Usage error | Invalid env name, missing loopzero.yaml |
| 2 | Build failure | npm install failed, Dockerfile error |
| 3 | Deploy failure | Health probe failed, ArgoCD sync error, concurrent rejection |
| 4 | Auth error | Not logged in, invalid token |
| 5 | Partial failure | Some apps deployed, some failed (multi-app) |
