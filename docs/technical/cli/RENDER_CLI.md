# Render CLI Reference — Look up supported commands and options.



This reference is automatically generated from the Render CLI source.

View documentation for any command in the CLI itself by running the following:

```shell
render help <command>
```

## Global options

| Option | Description |
| --- | --- |
| `--confirm` | Skip all confirmation prompts |
| `--output`, `-o` | Set output format to interactive, json, yaml, or text. Auto-switches to text on non-TTY |

## Top-level commands

###### `docs`

Open the Render docs in your browser

*Usage:*

```bash
render docs
```

*Examples:*

```bash
# Open Render documentation
render docs
```

*Options:*

[Global options](#global-options) only

###### `environments <projectID>`

List environments for a specified project in the active workspace. In interactive mode you can view each environment's individual services.

*Usage:*

```bash
render environments <projectID>
```

*Examples:*

```bash
# List environments for a project
render environments prj-abc123
```

*Options:*

[Global options](#global-options) only

###### `kv-cli [keyValueID|keyValueName]`

Open a redis-cli or valkey-cli session for a Render Key Value instance. This command only supports interactive mode.

You can optionally pass the key value ID or name as an argument. To pass arguments to redis-cli or valkey-cli, use:
  render kv-cli [keyValueID|keyValueName] -- [redis-cli args]

*Usage:*

```bash
render kv-cli [keyValueID|keyValueName]
```

*Examples:*

```bash
# Open an interactive kv-cli session
render kv-cli kv-abc123

# Pass through redis-cli arguments
render kv-cli kv-abc123 -- --scan
```

*Options:*

[Global options](#global-options) only

###### `login`

Log in to Render using the Render Dashboard

*Usage:*

```bash
render login
```

*Examples:*

```bash
# Authenticate with Render
render login
```

*Options:*

[Global options](#global-options) only

###### `logs`

View logs for services and datastores.

Use flags to filter logs by resource, instance, time, text, level, type, host, status code, method, or path. Unlike in the Render Dashboard, you can view logs for multiple resources at once.

In interactive mode you can update the filters and view logs in real time, or set `--tail=true` to stream new logs.

*Usage:*

```bash
render logs
```

*Examples:*

```bash
# Tail logs for a service
render logs --resources srv-abc123 --tail

# Query logs in a time range
render logs --resources srv-abc123 --start 2026-03-01T00:00:00Z --end 2026-03-01T01:00:00Z

# Output logs as JSON in non-interactive mode
render logs --resources srv-abc123 --output json
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--direction` | Set log query direction (backward or forward) |
| `--end` | Filter logs at or before the specified end time |
| `--host` | Filter logs by comma-separated host values |
| `--instance` | Filter logs by comma-separated instance IDs |
| `--level` | Filter logs by comma-separated log levels |
| `--limit` | Limit the number of logs returned |
| `--method` | Filter logs by comma-separated HTTP methods |
| `--path` | Filter logs by comma-separated request paths |
| `--resources`, `-r` | Filter logs by comma-separated resource IDs (Required in non-interactive mode) |
| `--start` | Filter logs at or after the specified start time |
| `--status-code` | Filter logs by comma-separated status codes |
| `--tail` | Stream new logs |
| `--task-id` | Filter logs by comma-separated task IDs |
| `--task-run-id` | Filter logs by comma-separated task run IDs |
| `--text` | Filter logs by comma-separated text values |
| `--type` | Filter logs by comma-separated log types |

###### `pgcli [postgresID|postgresName]`

Open a pgcli session to a Render Postgres database instance. This command only supports interactive mode.

You can optionally pass a database ID or name as an argument. To pass arguments to pgcli, use:
  render pgcli [postgresID|postgresName] -- [pgcli args]

*Usage:*

```bash
render pgcli [postgresID|postgresName]
```

*Examples:*

```bash
# Open an interactive pgcli session
render pgcli pg-abc123

# Pass through pgcli arguments
render pgcli pg-abc123 -- --csv -q
```

*Options:*

[Global options](#global-options) only

###### `projects`

Browse projects in the active workspace. In interactive mode, select a project to view its environments.

*Usage:*

```bash
render projects
```

*Examples:*

```bash
# List projects in JSON
render projects --output json
```

*Options:*

[Global options](#global-options) only

###### `psql [postgresID|postgresName]`

Open a psql session to a Render Postgres database.

Optionally pass the database ID or name as an argument. To pass arguments to psql, use:
  render psql [postgresID|postgresName] -- [psql args]

For non-interactive usage, use the `--command` flag:
  render psql [postgresID|postgresName] -c "SELECT * FROM users;" -o text

Additional psql flags can be passed after --:
  render psql [postgresID|postgresName] -c "SELECT 1;" -o json -- `--csv` -q

*Usage:*

```bash
render psql [postgresID|postgresName]
```

*Examples:*

```bash
# Open an interactive psql session
render psql pg-abc123

# Execute a SQL command in non-interactive mode
render psql pg-abc123 --command "SELECT * FROM users;" --output text

# Pass through psql arguments
render psql pg-abc123 -- --csv -q
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--command`, `-c` | Execute a SQL command in non-interactive mode |

###### `restart <resourceID>`

Restart a service by resource ID

*Usage:*

```bash
render restart <resourceID>
```

*Examples:*

```bash
# Restart a service
render restart srv-abc123

# Restart a service without confirmation prompts
render restart srv-abc123 --confirm
```

*Options:*

[Global options](#global-options) only

###### `ssh [serviceID|serviceName|instanceID]`

SSH into a service instance. This command only supports interactive mode.

You can specify the service ID, service name, or specific instance ID as an argument. To pass arguments to ssh, use:
  render ssh [serviceID|serviceName|instanceID] -- [ssh args]

*Usage:*

```bash
render ssh [serviceID|serviceName|instanceID]
```

*Examples:*

```bash
# Open an SSH session for a service
render ssh srv-abc123

# Connect to an ephemeral instance
render ssh srv-abc123 --ephemeral

# Pass through ssh arguments
render ssh srv-abc123 -- -L 5432:localhost:5432
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--ephemeral`, `-e` | Connect to an ephemeral instance |

###### `whoami`

Display information about the current user

*Usage:*

```bash
render whoami
```

*Examples:*

```bash
# Show the currently authenticated user
render whoami
```

*Options:*

[Global options](#global-options) only

###### `workspaces`

List workspaces available to your account

*Usage:*

```bash
render workspaces
```

*Examples:*

```bash
# List workspaces available to the current user
render workspaces
```

*Options:*

[Global options](#global-options) only

## Blueprints

###### `blueprints validate [file]`

Validate a Blueprint file for errors before committing.

Validates:
  - YAML syntax
  - Schema validation (Required fields, types)
  - Semantic validation (valid plans, regions, etc.)
  - Conflict checking against existing resources

*Usage:*

```bash
render blueprints validate [file]
```

*Examples:*

```bash
# Validate ./render.yaml
render blueprints validate

# Validate a specific Blueprint file
render blueprints validate ./my-blueprint.yaml

# Output validation results as JSON
render blueprints validate -o json
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--workspace`, `-w` | Validate against the specified workspace ID (defaults to current workspace) |

## Deploys

###### `deploys cancel <serviceID> <deployID>`

Cancel a running deploy

*Usage:*

```bash
render deploys cancel <serviceID> <deployID>
```

*Examples:*

```bash
# Cancel a running deploy
render deploys cancel srv-abc123 dep-xyz789
```

*Options:*

[Global options](#global-options) only

###### `deploys create [serviceID]`

Trigger a service deploy and stream logs in real time

*Usage:*

```bash
render deploys create [serviceID]
```

*Examples:*

```bash
# Trigger a deploy for a service
render deploys create srv-abc123

# Deploy a specific commit
render deploys create srv-abc123 --commit 0123abcd

# Wait until deploy completes
render deploys create srv-abc123 --wait
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--clear-cache` | Clear build cache before deploying |
| `--commit` | Deploy the specified commit ID |
| `--image` | Deploy the specified Docker image URL |
| `--wait` | Wait for deploy completion and exit non-zero if deploy fails |

###### `deploys list [serviceID]`

List deploys for a service

*Usage:*

```bash
render deploys list [serviceID]
```

*Examples:*

```bash
# List deploys for a service
render deploys list srv-abc123

# Browse deploys interactively
render deploys list
```

*Options:*

[Global options](#global-options) only

## Jobs

###### `jobs cancel <serviceID> <jobID>`

Cancel a running job

*Usage:*

```bash
render jobs cancel <serviceID> <jobID>
```

*Examples:*

```bash
# Cancel a running job
render jobs cancel srv-abc123 job-xyz789
```

*Options:*

[Global options](#global-options) only

###### `jobs create [serviceID]`

Create a new job for a service

*Usage:*

```bash
render jobs create [serviceID]
```

*Examples:*

```bash
# Create a job for a service
render jobs create srv-abc123 --start-command "bundle exec rake task"

# Create a job with a specific plan
# See https://render.com/docs/one-off-jobs for available job plans
render jobs create srv-abc123 --start-command "npm run worker" --plan-id plan-srv-006
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--plan-id` | Set the plan ID for the job (Optional) |
| `--start-command` | Set the job start command |

###### `jobs list [serviceID]`

List jobs for a service

*Usage:*

```bash
render jobs list [serviceID]
```

*Examples:*

```bash
# List jobs for a service
render jobs list srv-abc123

# Browse jobs interactively
render jobs list
```

*Options:*

[Global options](#global-options) only

## Services

###### `services`

Lists all services and datastores for the active workspace. In interactive mode, you can view logs, restart services, trigger deploys, SSH into instances, and connect to Render Postgres databases and Render Key Value instances.

*Usage:*

```bash
render services
```

*Aliases:* `service`

*Available commands:*

| Command | Description |
| --- | --- |
| [`create`](#services-create) | Create a new service or clone an existing one |
| [`instances`](#services-instances) | List instances for a service |
| [`update`](#services-update) | Update configuration for an existing service |

*Examples:*

```bash
# List all services
render services

# Output as JSON
render services --output json

# Filter by environment
render services -e env-abc123

# Include preview environments
render services --include-previews

# Combine filters
render services -e env-abc123,env-def456 --include-previews --output json
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--environment-ids`, `-e` | Filter services by comma-separated environment IDs |
| `--include-previews` | Include preview environments |

###### `services create`

Create a new service on Render. This command only runs in non-interactive modes. Provide configuration options with flags.

*Usage:*

```bash
render services create
```

*Examples:*

```bash
# Create a service from repository configuration
render services create --name my-api --type web_service --repo https://github.com/org/repo --runtime node --build-command "npm install" --start-command "npm start" --output json

# Clone configuration from an existing service
render services create --from srv-abc123 --name my-api-clone --output json
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--auto-deploy` | Enable auto-deploy |
| `--branch` | Set the Git branch |
| `--build-command` | Set the build command |
| `--build-filter-ignored-path` | Set build filter ignored paths (can be specified multiple times) |
| `--build-filter-path` | Set build filter paths (can be specified multiple times) |
| `--cron-command` | Set the cron command |
| `--cron-schedule` | Set the cron schedule |
| `--env-var` | Set environment variables in `KEY=VALUE` format (can be specified multiple times) |
| `--environment-id` | Set the environment ID |
| `--from` | Clone configuration from an existing service ID or name and override cloned values with other flags |
| `--health-check-path` | Set the health check path |
| `--image` | Set the Docker image URL |
| `--ip-allow-list` | Set IP allow list entries in `cidr=`..., `description=`... format (can be specified multiple times) |
| `--maintenance-mode` | Enable maintenance mode |
| `--maintenance-mode-uri` | Set the maintenance mode URI |
| `--max-shutdown-delay` | Set max shutdown delay in seconds |
| `--name` | Set the service name |
| `--num-instances` | Set the number of instances |
| `--plan` | Set the service plan |
| `--pre-deploy-command` | Set the pre-deploy command |
| `--previews` | Set preview generation mode |
| `--publish-directory` | Set the publish directory |
| `--region` | Set the deployment region |
| `--registry-credential` | Set the registry credential |
| `--repo` | Set the Git repository URL |
| `--root-directory` | Set the root directory |
| `--runtime` | Set the runtime environment |
| `--secret-file` | Set secret files in NAME:`LOCAL_PATH` format (can be specified multiple times) |
| `--start-command` | Set the start command |
| `--type` | Set the service type |

###### `services instances [serviceID]`

List instances for a service

*Usage:*

```bash
render services instances [serviceID]
```

*Examples:*

```bash
# List instances for a service
render services instances srv-abc123

# Browse instances interactively
render services instances
```

*Options:*

[Global options](#global-options) only

###### `services update <service>`

Update a service on Render. This command only runs in non-interactive modes.

Provide configuration updates with flags.

*Usage:*

```bash
render services update <service>
```

*Examples:*

```bash
# Rename a service
render services update my-service --name my-new-name --output json

# Change a service plan
render services update srv-abc123 --plan pro --output json
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--auto-deploy` | Enable auto-deploy |
| `--branch` | Git branch |
| `--build-command` | Build command |
| `--build-filter-ignored-path` | Build filter ignored path (can be specified multiple times) |
| `--build-filter-path` | Build filter path (can be specified multiple times) |
| `--cron-command` | Cron command |
| `--cron-schedule` | Cron schedule |
| `--health-check-path` | Health check path |
| `--image` | Docker image URL |
| `--ip-allow-list` | IP allow list entry in `cidr=`..., `description=`... format (can be specified multiple times) |
| `--maintenance-mode` | Enable maintenance mode |
| `--maintenance-mode-uri` | Maintenance mode URI |
| `--max-shutdown-delay` | Max shutdown delay in seconds |
| `--name` | Service name |
| `--num-instances` | Number of instances |
| `--plan` | Service plan |
| `--pre-deploy-command` | Pre-deploy command |
| `--previews` | Preview generation mode |
| `--publish-directory` | Publish directory |
| `--registry-credential` | Registry credential |
| `--repo` | Git repository URL |
| `--root-directory` | Root directory |
| `--runtime` | Runtime environment |
| `--start-command` | Start command |

## Skills

###### `skills`

Install and manage Render agent skills for AI coding tools such as Claude Code, Codex, OpenCode, and Cursor. Skills add deployment, debugging, and monitoring capabilities to your AI coding assistant.

*Usage:*

```bash
render skills
```

*Available commands:*

| Command | Description |
| --- | --- |
| [`install`](#skills-install) | Install Render skills to AI coding tools |
| [`list`](#skills-list) | List installed Render skills and detected tools |
| [`remove`](#skills-remove) | Remove installed Render skills from AI coding tools |
| [`update`](#skills-update) | Update previously installed Render skills |

*Options:*

[Global options](#global-options) only

###### `skills install`

Install Render agent skills from https://github.com/render-oss/skills to detected AI coding tools.

Supported tools: Claude Code, Codex, OpenCode, Cursor.

Skills can be installed at two scopes:
  - user:    Install to ~/.{tool}/skills/ (default, current user only)
  - project: Install to ./.{tool}/skills/ (committed to Git, all collaborators)

By default an interactive prompt lets you pick scope, tools, and skills. Use `--scope`, `--tool`, and `--skill` flags to skip the prompts (useful for CI).

*Usage:*

```bash
render skills install
```

*Examples:*

```bash
# Install skills interactively
render skills install

# Install for a specific tool and scope
render skills install --tool cursor --scope project

# Preview install changes
render skills install --dry-run
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--dry-run` | Show what would be installed without making changes |
| `--scope` | Set installation scope to user or project (defaults to user) |
| `--skill` | Install specific skills only (use `--skill` multiple times) |
| `--tool` | Install skills to a specific tool only (claude, codex, opencode, or cursor) |

###### `skills list`

List installed Render skills and the AI tools they've been installed in. This reads from local state only, so the command doesn't require network access.

Use `--scope` to filter by installation scope (user or project).

*Usage:*

```bash
render skills list
```

*Examples:*

```bash
# List all installed skills
render skills list

# List project-scoped skills only
render skills list --scope project
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--scope` | Filter skills by installation scope (user or project) |

###### `skills remove`

Remove previously installed Render skills from detected AI coding tools.

By default an interactive prompt lets you pick which skills to remove. Use `--skill` and `--all` flags to skip the prompts.

Use `--scope` to remove from a specific scope (user or project).

*Usage:*

```bash
render skills remove
```

*Examples:*

```bash
# Remove skills interactively
render skills remove

# Remove specific skills
render skills remove --skill render-deploy --skill render-debug

# Remove all project-scoped skills
render skills remove --all --scope project
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--all` | Remove all installed Render skills |
| `--scope` | Remove skills from the specified scope (user or project) |
| `--skill` | Remove specific skills only (use `--skill` multiple times) |
| `--tool` | Remove skills from a specific tool only (claude, codex, opencode, or cursor) |

###### `skills update`

Reinstall Render skills using the tool and skill selections saved by a previous "render skills install" run.

This fetches the latest version of each selected skill from the skills repository, compares with installed versions, and updates any that have changed.

Use `--scope` to update skills at a specific scope (user or project).

*Usage:*

```bash
render skills update
```

*Examples:*

```bash
# Update installed skills
render skills update

# Force reinstall all skills
render skills update --force

# Update project-scoped skills
render skills update --scope project
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--force` | Reinstall all skills even if already up to date |
| `--scope` | Update skills at the specified scope (user or project) |

## Workflows

###### `workflows create`

Create a new workflow service on Render.

In interactive mode, a form guides you through the required fields.
In non-interactive mode, provide all required config with flags.

*Usage:*

```bash
render workflows create
```

*Examples:*

```bash
render workflows create
render workflows create --name my-workflow --repo https://github.com/org/repo --build-command "npm install" --runtime node --run-command "npm start" --region oregon -o json
render workflows create --repo . --name my-workflow --build-command "npm install" --runtime node --run-command "npm start"
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--auto-deploy-trigger` | Autodeploy behavior (commit, off, checksPass; default: commit) |
| `--branch` | Git branch (Optional) |
| `--build-command` | Build command. Required in non-interactive mode. |
| `--name` | Workflow name. Required in non-interactive mode. |
| `--region` | Deployment region (default: oregon) |
| `--repo` | Git repository URL, or a local directory path (e.g. '.') to resolve via the repo's origin remote. Required in non-interactive mode. |
| `--root-directory` | Root directory in the repository (Optional) |
| `--run-command` | Command to run the workflow. Required in non-interactive mode. |
| `--runtime` | Runtime (node, python, go, ruby, elixir). Required in non-interactive mode. |

###### `workflows dev -- <command to start a workflow service>`

Start a workflow service in development mode for local testing.
Required input: -- <command to start a workflow service>

This command runs your workflow service locally on port 8120, allowing you to list and run tasks without deploying to Render. Task runs and their logs are stored in memory, so you can query them after tasks complete.

The command will spawn a new subprocess with your specified command whenever it needs to run a task or list the defined tasks.

To interact with the local task server:

- Use the `--local` flag with other task commands (e.g., 'render workflows tasks list `--local`')

- Or set `RENDER_USE_LOCAL_DEV=true` when using the workflow client SDK

To use a different port:

- Specify `--port` when starting the dev server

- Then use `--port` with other task commands, or set `RENDER_LOCAL_DEV_URL` in the SDK

*Usage:*

```bash
render workflows dev -- <command to start a workflow service>
```

*Examples:*

```bash
# Start local workflow development server
render workflows dev -- "python main.py"

# Start local workflow development server on a custom port
render workflows dev --port 9000 -- "npm start"

# List local tasks from another terminal
render workflows tasks list --local
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--debug` | Print detailed workflow task execution events |
| `--port` | Set the port of the local task server |

###### `workflows init`

Scaffold a new Render Workflows project with example tasks.

Creates a working example project with task definitions, dependencies, and a README with instructions for local development and Client SDK integration.

In interactive mode you'll be prompted to select a language, template, output directory, and optional features. Use `--confirm` to skip all prompts and accept defaults, or pass individual flags to skip specific prompts.

With `--confirm` or non-interactive output (-o text/json/yaml), dependencies are installed and Git is initialized by default. Pass `--install-deps=false` or `--git=false` to opt out.

*Usage:*

```bash
render workflows init
```

*Examples:*

```bash
# Scaffold with default settings
render workflows init

# Skip prompts and use Python
render workflows init --confirm --language python

# Skip prompts and disable Git initialization
render workflows init --confirm --language python --git=false

# Customize output directory and enable optional features
render workflows init --language python --dir my-project --install-deps --git

# Use Node.js with a custom directory
render workflows init --language node --dir my-project
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--dir` | Output directory (default: ./workflows-demo) |
| `--git` | Initialize a Git repository (default true with `--confirm`) |
| `--install-agent-skill` | Install the Workflows agent skill for detected AI coding tools |
| `--install-deps` | Install dependencies after scaffolding (default true with `--confirm`) |
| `--language` | Language for the Render Workflows project (python, node) |
| `--template` | Template to scaffold (defaults to the repo's default template) |

###### `workflows list`

List workflow services in your workspace

*Usage:*

```bash
render workflows list
```

*Examples:*

```bash
# List workflows in the active workspace
render workflows list
```

*Options:*

[Global options](#global-options) only

###### `workflows runs`

View task run executions.

A task run represents a single execution of a task with specific input parameters. Use these commands to view task run history and inspect details.

To start a new task run, use:
  render workflows tasks start

*Usage:*

```bash
render workflows runs
```

*Available commands:*

| Command | Description |
| --- | --- |
| [`cancel`](#workflows-runs-cancel) | Cancel a running task run |
| [`list`](#workflows-runs-list) | List all execution runs for a specific task |
| [`show`](#workflows-runs-show) | Show detailed information about a task run |

*Examples:*

```bash
# List task runs for a task
render workflows runs list tsk-1234

# Show details for a task run
render workflows runs show tr-1234

# Cancel a task run
render workflows runs cancel trn-1234
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows runs cancel [taskRunID]`

Cancel an in-progress task run.

Use `--local` to cancel a task run in the local workflow development server.

*Usage:*

```bash
render workflows runs cancel [taskRunID]
```

*Examples:*

```bash
# Cancel a remote task run
render workflows runs cancel trn-abc123

# Cancel a task run in the local dev server
render workflows runs cancel --local trn-xyz789
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows runs list [taskID]`

List all execution runs for a specific task.

A task run represents a single execution of a task with specific input parameters. This command shows the history of all runs for a given task.

You can specify the task by its workflow slug and task name (e.g., my-workflow/my-task).

In interactive mode, you will be prompted to select a task if not provided.

*Usage:*

```bash
render workflows runs list [taskID]
```

*Examples:*

```bash
# List task runs by task ID
render workflows runs list tsk-1234

# List task runs by task slug
render workflows runs list my-workflow/my-task

# List task runs from local workflow development server
render workflows runs list --local my-task
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows runs show [taskRunID]`

Display detailed information about a specific task run execution.

This command shows comprehensive information about a task run, including:

- Task run ID and status

- Input parameters provided

- Output or error result

- Start and completion timestamps

The task run ID is returned when you execute a task with:
  render workflows tasks start

In interactive mode, you will be prompted to select a task run if not provided.

*Usage:*

```bash
render workflows runs show [taskRunID]
```

*Examples:*

```bash
# Show details for a task run
render workflows runs show tr-1234

# Show details from local workflow development server
render workflows runs show --local tr-5678
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows tasks`

List tasks and start task runs

*Usage:*

```bash
render workflows tasks
```

*Available commands:*

| Command | Description |
| --- | --- |
| [`list`](#workflows-tasks-list) | List tasks in a workflow version |
| [`start`](#workflows-tasks-start) | Start a task run with the provided input |

*Examples:*

```bash
# List tasks in a workflow version
render workflows tasks list wfv-1234

# Start a task run
render workflows tasks start my-task --input='["arg1"]'
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows tasks list [workflowVersionID]`

List all tasks defined in a workflow version.

Tasks are user-defined functions registered with the Render Workflows SDK. Each time you release a workflow service, Render creates a new workflow version and registers all tasks it finds in that version.

In interactive mode, you will be prompted to select a workflow if not provided.

Local Development:
When using the `--local` flag, you don't need to provide a workflow version ID. Instead, the command connects to your local dev server (default port 8120) to list tasks from your running workflow service. Start the dev server first with:
  render workflows dev -- "<your command>"

*Usage:*

```bash
render workflows tasks list [workflowVersionID]
```

*Examples:*

```bash
# List tasks for a workflow version
render workflows tasks list wfv-1234

# List tasks from local workflow development server
render workflows tasks list --local
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows tasks start [taskSlug]`

Start a task with the provided input. In non-interactive mode, provide input with `--input` or `--input-file`.

You can specify the task by its workflow slug and task name (e.g., my-workflow/my-task)

Input Format:
The input should be a JSON array where each element is an argument to the task. For example, if your task takes two arguments, provide: ["arg1", "arg2"]

You can provide input via:

- `--input` with inline JSON

- `--input-file` with a path to a JSON file

In interactive mode, you will be prompted to select the task and provide the input.

*Usage:*

```bash
render workflows tasks start [taskSlug]
```

*Examples:*

```bash
# Start a task run with inline JSON input
render workflows tasks start tsk-1234 --input='["arg1", "arg2"]'

# Start a task run with input from a file
render workflows tasks start my-task --input-file=input.json

# Start a task run against local workflow development server
render workflows tasks start my-task --local --input='["test"]'
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--input` | Provide task input as a JSON array |
| `--input-file` | Read task input from a JSON file path |
| `--local` | Run against the local workflow development server |
| `--port` | Set the port of the local task server |

###### `workflows versions`

List and release workflow versions

*Usage:*

```bash
render workflows versions
```

*Available commands:*

| Command | Description |
| --- | --- |
| [`list`](#workflows-versions-list) | List versions of a workflow |
| [`release`](#workflows-versions-release) | Release a new workflow version |

*Examples:*

```bash
# List versions for a workflow
render workflows versions list wf-abc123

# Release a new workflow version
render workflows versions release wf-abc123
```

*Options:*

[Global options](#global-options) only

###### `workflows versions list [workflowID]`

List all versions of a workflow service.

Each time you release a workflow service, Render creates a new workflow version. A version represents a specific snapshot of your workflow service code and its registered tasks at the time of release.

This command displays all versions for a workflow, showing:

- Version ID

- Creation timestamp

- Associated tasks

In interactive mode, you will be prompted to select a workflow if not provided.

*Usage:*

```bash
render workflows versions list [workflowID]
```

*Examples:*

```bash
# List versions by workflow ID
render workflows versions list wf-1234

# List versions by workflow slug
render workflows versions list my-workflow-slug
```

*Options:*

[Global options](#global-options) only

###### `workflows versions release [workflowID]`

Release a new version of a workflow service.

This command triggers a new release of your workflow service on Render. With a new release, Render:

1. Pulls the latest code from your repository (or a specific commit)

2. Builds your workflow service

3. Registers all tasks it finds in the service

4. Creates a new workflow version

You can optionally specify a commit ID to release a specific version of your code.

In interactive mode, you will be prompted to:

- Select a workflow if not provided

- Confirm the release

*Usage:*

```bash
render workflows versions release [workflowID]
```

*Examples:*

```bash
# Release a new version
render workflows versions release wf-1234

# Release from a specific commit
render workflows versions release wf-1234 --commit abc123

# Wait for release completion
render workflows versions release wf-1234 --wait
```

*Options:*

[Global options](#global-options), plus:

| Option | Description |
| --- | --- |
| `--commit` | Release the specified commit ID |
| `--wait` | Wait for release completion and exit non-zero if release fails |

## Workspace

###### `workspace current`

Show the currently selected workspace

*Usage:*

```bash
render workspace current
```

*Examples:*

```bash
# Show the active workspace
render workspace current
```

*Options:*

[Global options](#global-options) only

###### `workspace set [workspaceName|workspaceID]`

Set the CLI's active workspace. All CLI commands run against the active workspace.

The active workspace is saved in a config file specified by the `RENDER_CLI_CONFIG_PATH` environment variable. If unspecified, the config file is saved in $HOME/.render/cli.yaml.

*Usage:*

```bash
render workspace set [workspaceName|workspaceID]
```

*Options:*

[Global options](#global-options) only

