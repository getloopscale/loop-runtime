# Sprint 2 — Manual Testing Guide

> **Audience:** Anyone — no Kubernetes or Go experience required.
> **Time:** ~30 minutes (infra already running) or ~45 minutes (from scratch).
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
| 8 | **Deploy Turborepo monorepo (3 apps)** | `loopzero init` + `loopzero deploy` | Detects types, deploys 3 apps, 2 URLs | [ ] |
| 9 | Deploy JSON (3 apps) | `loopzero deploy --json` | JSON with 3 apps, correct types/URLs | [ ] |
| 10 | Deploy no-wait | `loopzero deploy --no-wait` | `Deploy started (build-xxx)`, exit 0 | [ ] |
| 11 | Deploy to staging | `loopzero deploy --env staging` | Exit 0 | [ ] |
| 12 | Deploy UPPERCASE env | `loopzero deploy --env PRODUCTION` | `Environment names must be lowercase.`, exit 1 | [ ] |
| 13 | Deploy without git | Run outside a git repo | `Run from inside a git repository.`, exit 1 | [ ] |
| 14 | Deploy without loopzero.yaml | Run in empty git repo | `No loopzero.yaml found. Run: loopzero init`, exit 1 | [ ] |
| 15 | Deploy without login | logout → `loopzero deploy` | Exit 4 | [ ] |
| 16 | Deploy unknown app | `loopzero deploy --app fake` | Exit 1 before upload | [ ] |

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

### Step 5: Start the dev server

In the **same terminal** (it stays in foreground):

```bash
tilt up
```

Wait until the Tilt dashboard (http://localhost:10350) shows all resources green. This builds the API server and deploys it to the local cluster.

### Step 6: Open a new terminal for testing

Open a **second terminal**. All test commands run here.

```bash
cd /home/paulo/Projetos/loopzero/loopzero
```

### Step 7: Build the CLI

```bash
cd cli && npm install && npm run build && cd ..
```

### Step 8: Create the `loopzero` command

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

### Step 9: Seed the test user

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
  res.writeHead(200);
  res.end('hello from loopzero');
}).listen(process.env.PORT || 3000);
EOF

git init && git add -A && git commit -m "my first app"
```

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

### Test 8: Deploy a real monorepo (the main test)

The previous tests used a single-file app. Now we'll create a **real Turborepo monorepo** with multiple apps and shared packages — the kind of project LoopZero is designed for.

#### Create the monorepo

```bash
rm -rf /tmp/my-app  # remove simple app
mkdir -p /tmp/my-app && cd /tmp/my-app

# Root: Turborepo + npm workspaces
cat > package.json << 'ROOTPKG'
{
  "name": "acme-platform",
  "private": true,
  "workspaces": ["apps/*", "packages/*"],
  "devDependencies": { "turbo": "^2.0.0", "typescript": "^5.4.0" }
}
ROOTPKG

cat > turbo.json << 'TURBO'
{ "$schema": "https://turbo.build/schema.json", "tasks": { "build": { "dependsOn": ["^build"] } } }
TURBO

# App 1: API server (Express)
mkdir -p apps/api/src
cat > apps/api/package.json << 'EOF'
{ "name": "@acme/api", "private": true, "dependencies": { "express": "^4.18.0", "@acme/shared": "*" } }
EOF
cat > apps/api/src/index.ts << 'EOF'
import express from 'express';
const app = express();
app.get('/health', (_, res) => res.json({ status: 'ok' }));
app.get('/api/v1/users', (_, res) => res.json([{ id: '1', name: 'Alice' }]));
app.listen(process.env.PORT || 3000);
EOF

# App 2: Web dashboard (Next.js)
mkdir -p apps/web/src/pages
cat > apps/web/package.json << 'EOF'
{ "name": "@acme/web", "private": true, "dependencies": { "next": "^14.2.0", "react": "^18.3.0", "@acme/shared": "*" } }
EOF
cat > apps/web/next.config.js << 'EOF'
module.exports = { reactStrictMode: true };
EOF
cat > apps/web/src/pages/index.tsx << 'EOF'
export default function Home() { return <h1>Acme Dashboard</h1>; }
EOF

# App 3: Background worker (no HTTP, no port)
mkdir -p apps/worker/src
cat > apps/worker/package.json << 'EOF'
{ "name": "@acme/worker", "private": true, "dependencies": { "bullmq": "^5.0.0", "@acme/shared": "*" } }
EOF
cat > apps/worker/src/index.ts << 'EOF'
console.log('Worker processing orders...');
setInterval(() => console.log('Processing...'), 5000);
EOF

# Shared package: types and utils
mkdir -p packages/shared/src
cat > packages/shared/package.json << 'EOF'
{ "name": "@acme/shared", "private": true, "main": "src/index.ts" }
EOF
cat > packages/shared/src/index.ts << 'EOF'
export interface User { id: string; name: string; email: string; }
export interface Order { id: string; userId: string; status: 'pending' | 'shipped'; }
export function formatCurrency(n: number): string { return '$' + n.toFixed(2); }
EOF

# Git
cat > .gitignore << 'EOF'
node_modules/
dist/
.next/
.turbo/
EOF

git init && git add -A && git commit -m "Acme Platform monorepo"
```

#### Initialize the project

```bash
loopzero init --yes --name acme-platform
```

**You should see:**

```
Detected: turborepo monorepo
  Shared packages (not deployed): database, shared
  apps/api → express (server, port 3000)
  apps/web → nextjs (frontend)
  apps/worker → node (worker)

Generated loopzero.yaml with 3 apps.
```

**What to verify (no manual editing needed!):**
- `packages/shared` and `packages/database` are **automatically excluded** — they're libraries, not deployable apps
- `api` is detected as **server** (Express, port 3000)
- `web` is detected as **frontend** (Next.js)
- `worker` is detected as **worker** (name contains "worker")
- The loopzero.yaml has the correct types without you touching it

Check the file yourself:

```bash
cat loopzero.yaml
```

Expected:
```yaml
project: acme-platform

apps:
  api:
    path: apps/api
    framework: express
    port: 3000

  web:
    path: apps/web
    framework: nextjs
    type: frontend

  worker:
    path: apps/worker
    framework: node
    type: worker
```

#### Deploy the monorepo

Commit and deploy — no editing required:

```bash
git add -A && git commit -m "add loopzero config"
loopzero deploy
echo "Exit code: $?"
```

**You should see:**
```
Deploy completed in 0s
  api: https://api.acme-platform.acme.loopzero.example
  web: https://web.acme-platform.acme.loopzero.example
Exit code: 0
```

**What to verify:**
- **api** has a URL (it's a server with a port)
- **web** has a URL (it's a frontend)
- **worker** does NOT appear with a URL (it's a background process — correct!)
- Exit code is 0
- **You did NOT edit loopzero.yaml manually** — `loopzero init` got it right automatically

> **Note:** The URLs are simulated — Sprint 2 uses a fake build pipeline. The important thing is that `loopzero login → loopzero init → loopzero deploy` works as a one-command flow on a real Turborepo monorepo with 3 different app types and 2 shared packages, with zero manual configuration.

---

### Test 9: Deploy with JSON output

```bash
loopzero deploy --json
echo "Exit code: $?"
```

**You should see** JSON with **3 apps** (not 1):
```json
{
  "status": "success",
  "deploy_id": "d-abcd",
  "apps": [
    { "name": "api", "status": "deployed", "url": "https://api.acme-platform.acme.loopzero.example" },
    { "name": "web", "status": "deployed", "url": "https://web.acme-platform.acme.loopzero.example" },
    { "name": "worker", "status": "deployed", "url": "" }
  ]
}
```

**Verify:**
- `status` is `"success"`
- `deploy_id` starts with `d-` followed by 4 hex characters (like `d-8f3a`)
- **3 apps** in the array — `api` and `web` have URLs, `worker` has empty URL
- Exit code is 0

---

### Test 10: Deploy with --no-wait

```bash
loopzero deploy --no-wait
echo "Exit code: $?"
```

**You should see:**
```
Deploy started (build-xxxxxxxx). Run: loopzero status to check progress.
Exit code: 0
```

This returns immediately without waiting for the build to finish. Exit code is always 0 if the submit was accepted.

---

### Test 11: Deploy to a different environment

```bash
loopzero deploy --env staging
echo "Exit code: $?"
```

**You should see:** `Deploy completed`, exit code 0. The deploy targets `staging` instead of the default `production`.

---

### Test 12: Deploy with invalid environment name

```bash
loopzero deploy --env PRODUCTION
echo "Exit code: $?"
```

**You should see:** `Environment names must be lowercase.`, exit code **1**.

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

**You should see:** `Not logged in. Run: loopzero login`, exit code **4**.

Log back in:

```bash
loopzero login --token $MY_TOKEN
```

---

### Test 16: Deploy unknown app

```bash
loopzero deploy --app nonexistent
echo "Exit code: $?"
```

**You should see:** an error mentioning `nonexistent` and listing available apps, exit code **1**. This fails **before** uploading any code (fail-fast behavior).

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

Run Step 8 again to create the `/tmp/loopzero` wrapper. Or make sure `/tmp` is in your PATH:

```bash
export PATH="/tmp:$PATH"
```

### `loopzero deploy` hangs forever

The deploy waits up to 5 minutes for the build to complete. If it hangs beyond that, the API server may have restarted. Press `Ctrl+C` and try again.

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

## What Sprint 2 does NOT test (known limitations)

These are intentional for Sprint 2. They are not bugs:

| What | Why |
|------|-----|
| Real app running at the URL | Builds use a simulated pipeline (no real Argo Workflows) |
| Real container image | No Kaniko build — image is faked |
| Real GitOps push | No actual push to Gitea — faked |
| Real ArgoCD sync | No actual sync to workload cluster — faked |
| Health probe on real URL | Probe always passes — faked |
| `loopzero logs`, `loopzero status`, `loopzero rollback` | These commands are Sprint 3-4 |

The simulated pipeline validates that the **entire flow** works: CLI packages code → API creates build → build completes → release created → release activated → URL returned. Sprint 3+ will replace the fakes with real infrastructure.
