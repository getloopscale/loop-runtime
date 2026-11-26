// Package renderschemagensql renders api/internal/db/schema.gen.sql via
// an ephemeral PostgreSQL 16 container, replays golang-migrate migrations,
// dumps the schema, post-processes it deterministically, and validates
// the result with sqlc compile.
//
// T0.2 of plan 2026-05-29-sqlc-migration-plan v1.2.
// Migrated from infra/scripts/ci/render-schema-gen-sql.sh (M-Migrate2
// continuation 2026-07-07). The subprocess boundaries (docker, migrate,
// pg_dump inside container, sqlc) are legitimate — these are canonical
// tools that read/write real state. What the Go port BUYS: typed
// exit contract, structured error surfaces, testable configuration,
// deterministic trap cleanup via defer, no host pg_dump dependency
// (still exec'd inside the container).
//
// Preserved edge cases:
//   - EC-4: defer cleanup guarantees container removal on abort/SIGINT
//   - EC-17: --publish 0:5432 random host port avoids k3d PG conflict
//   - EC-2:  sqlc compile pre-flight against rendered schema
//
// Security (M-5): the DB role for migration is non-superuser (`loop-runtime`);
// pg_dump uses --no-owner --no-privileges --no-comments; post-process
// strips residual OWNER/SET/serial-comment lines defense-in-depth.
package renderschemagensql

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

const (
	pgImage              = "postgres:16"
	dbName               = "loop_runtime"
	dbUser               = "loop-runtime"
	readyBudgetSec       = 30
	golangMigrateVersion = "v4.18.1"
	migrationsSubdir     = "api/internal/db/migrations"
	schemaOutSubdir      = "api/internal/db/schema.gen.sql"
	sqlcConfigSubdir     = "api/internal/db/sqlc.yaml"
	probeQuerySubdir     = "api/internal/db/queries/_phase0_render_probe.sql"
)

// Config controls the render.
type Config struct {
	RepoRoot string
	// AdvisoryOnly returns Warn=true instead of running when docker/migrate/sqlc
	// are missing on PATH. Useful for CI environments that don't have the
	// full toolchain but still want the gate to exit 0.
	AdvisoryOnly bool
}

// Outcome is the summary.
type Outcome struct {
	// Warn is true when a required tool is missing and AdvisoryOnly is set,
	// OR when the render was skipped for a legitimate reason.
	Warn        bool
	WarnReason  string
	SchemaLines int
	// Diagnostic is the tool stderr on failure.
	Diagnostic string
}

// Verdict is Skip when a required tool is missing and the run is advisory,
// and Pass otherwise: a check or render that could not complete is an error,
// not an outcome.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Warn {
		return verdict.Skip
	}
	return verdict.Pass
}

// ErrRenderFailed is emitted when any stage of the render pipeline fails.
var ErrRenderFailed = errors.New("schema.gen.sql render failed")

// ErrMissingTool is emitted when a required tool is not on PATH (unless
// AdvisoryOnly is set, in which case Warn=true is returned instead).
var ErrMissingTool = errors.New("required tool missing on PATH")

// Render runs the full pipeline. Returns Outcome + err.
//
// Steps (mirroring the bash script §1-§7):
//  1. Pre-flight tool checks (docker, migrate, sqlc)
//  2. Spin ephemeral PG 16 container
//  3. Wait for PG ready
//  4. golang-migrate replay
//  5. pg_dump (executed INSIDE container)
//  6. Post-process for determinism + security
//  7. sqlc compile pre-flight
func Render(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	// Step 1: pre-flight tool checks.
	requiredTools := []string{"docker", "migrate", "sqlc"}
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			if cfg.AdvisoryOnly {
				return Outcome{Warn: true, WarnReason: fmt.Sprintf("%s not installed on PATH", tool)}, nil
			}
			return Outcome{}, fmt.Errorf("%w: %s (bash script §1 pre-flight)", ErrMissingTool, tool)
		}
	}

	migrationsDir := filepath.Join(root, migrationsSubdir)
	if _, err := os.Stat(migrationsDir); err != nil {
		return Outcome{}, fmt.Errorf("migrations dir not found: %s (run from repo root)", migrationsSubdir)
	}

	dbPass := ephemeralPassword()

	// Step 2: spin ephemeral PG container.
	pgCID, err := runDocker(pgImage, dbPass)
	if err != nil {
		return Outcome{}, fmt.Errorf("docker run PG: %w", err)
	}
	// Step trap: defer cleanup — mirrors bash `trap cleanup EXIT INT TERM`.
	defer func() {
		_ = exec.Command("docker", "rm", "-f", pgCID).Run()
	}()

	// Resolve assigned random host port.
	hostPort, err := resolveHostPort(pgCID)
	if err != nil {
		return Outcome{}, fmt.Errorf("resolve host port: %w", err)
	}

	// Step 3: wait for PG ready.
	if err := waitPGReady(pgCID, readyBudgetSec); err != nil {
		return Outcome{}, fmt.Errorf("PG readiness: %w", err)
	}

	// Step 4: golang-migrate replay.
	dsn := fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable", dbUser, dbPass, hostPort, dbName)
	if err := runMigrate(migrationsDir, dsn); err != nil {
		return Outcome{}, fmt.Errorf("golang-migrate: %w", err)
	}

	// Step 5: pg_dump INSIDE container.
	rawDump, err := pgDumpInContainer(pgCID, dbPass)
	if err != nil {
		return Outcome{}, fmt.Errorf("pg_dump: %w", err)
	}

	// Step 6: post-process.
	cleaned := postProcess(rawDump)
	schemaOut := filepath.Join(root, schemaOutSubdir)
	if err := os.WriteFile(schemaOut, []byte(cleaned), 0o644); err != nil {
		return Outcome{}, fmt.Errorf("write schema.gen.sql: %w", err)
	}
	schemaLines := strings.Count(cleaned, "\n")

	// Step 7: sqlc compile pre-flight with a throwaway probe.
	if err := sqlcCompileWithProbe(root); err != nil {
		return Outcome{SchemaLines: schemaLines}, fmt.Errorf("sqlc compile: %w", err)
	}

	return Outcome{SchemaLines: schemaLines}, nil
}

// Check is a lightweight advisory-mode wrapper for `loop-runtime-ops quality`.
// The full render is destructive (spins up docker, mutates schema.gen.sql).
// The check mode instead validates that the necessary toolchain is present
// AND the migrations + sqlc config exist. Operators run `--render` for the
// real thing.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	// Toolchain presence check.
	missing := []string{}
	for _, tool := range []string{"docker", "migrate", "sqlc"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return Outcome{Warn: true, WarnReason: fmt.Sprintf("advisory-only: missing tools %v", missing)}, nil
	}
	// Config presence check.
	for _, sub := range []string{migrationsSubdir, sqlcConfigSubdir} {
		if _, err := os.Stat(filepath.Join(root, sub)); err != nil {
			return Outcome{Warn: true, WarnReason: fmt.Sprintf("advisory-only: %s not found", sub)}, nil
		}
	}
	return Outcome{Warn: false}, nil
}

// ephemeralPassword generates a random password like the bash `${RANDOM}` interpolation.
func ephemeralPassword() string {
	return fmt.Sprintf("rendertest-%d", rand.Intn(32767))
}

// runDocker executes `docker run --rm -d --publish 0:5432 -e POSTGRES_DB ...`
// and returns the container id.
func runDocker(image, password string) (string, error) {
	cmd := exec.Command("docker", "run", "--rm", "-d",
		"--publish", "0:5432",
		"-e", fmt.Sprintf("POSTGRES_DB=%s", dbName),
		"-e", fmt.Sprintf("POSTGRES_USER=%s", dbUser),
		"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
		image,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	cid := strings.TrimSpace(stdout.String())
	if cid == "" {
		return "", fmt.Errorf("empty container id")
	}
	return cid, nil
}

// resolveHostPort maps container port 5432/tcp to the assigned host port.
func resolveHostPort(cid string) (string, error) {
	out, err := exec.Command("docker", "port", cid, "5432/tcp").Output()
	if err != nil {
		return "", err
	}
	// Output like `0.0.0.0:32768` — take the LAST colon-separated field of the first line.
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	i := strings.LastIndex(line, ":")
	if i < 0 || i+1 >= len(line) {
		return "", fmt.Errorf("could not parse host port from %q", line)
	}
	return line[i+1:], nil
}

// pgReadyArgs is the readiness probe of the container. It asks over TCP: the
// image's init phase runs a temporary server on the unix socket alone and then
// restarts it, so a socket probe answers before the server migrate connects to.
func pgReadyArgs(cid string) []string {
	return []string{"exec", cid, "pg_isready", "-h", "127.0.0.1", "-U", dbUser, "-d", dbName}
}

// waitPGReady polls `docker exec cid pg_isready` up to budget seconds.
func waitPGReady(cid string, budgetSec int) error {
	for i := 0; i < budgetSec; i++ {
		cmd := exec.Command("docker", pgReadyArgs(cid)...)
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("PG not ready after %ds", budgetSec)
}

// runMigrate runs `migrate -path <dir> -database <dsn> up`.
func runMigrate(dir, dsn string) error {
	cmd := exec.Command("migrate", "-path", dir, "-database", dsn, "up")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// pgDumpInContainer executes pg_dump inside the container and returns the raw dump.
func pgDumpInContainer(cid, password string) (string, error) {
	cmd := exec.Command("docker", "exec",
		"-e", fmt.Sprintf("PGPASSWORD=%s", password),
		cid,
		"pg_dump",
		"--host=localhost", "--port=5432",
		"--username="+dbUser, "--dbname="+dbName,
		"--no-owner", "--no-privileges",
		"--schema-only", "--no-comments",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// postProcess ports the bash sed + awk pipeline: strips ALTER…OWNER TO,
// pg_dump version banner, SET…, SELECT pg_catalog.set_config, \restrict,
// \unrestrict, orphan `--` lines, trailing whitespace, and collapses
// multiple blank lines to one.
func postProcess(raw string) string {
	// Step 1: sed-equivalent line filter.
	// Order: check each pattern per line; a match drops the line, else it stays.
	dropPatterns := []*regexp.Regexp{
		regexp.MustCompile(`^ALTER (TABLE|SEQUENCE|TYPE|FUNCTION|SCHEMA) .* OWNER TO `),
		regexp.MustCompile(`^-- Dumped (from|by) `),
		regexp.MustCompile(`^SET .* = `),
		regexp.MustCompile(`^SELECT pg_catalog\.set_config`),
		regexp.MustCompile(`^\\restrict `),
		regexp.MustCompile(`^\\unrestrict `),
		regexp.MustCompile(`^--$`),
	}
	trailingWS := regexp.MustCompile(`[[:space:]]+$`)

	var filtered []string
	for _, line := range strings.Split(raw, "\n") {
		drop := false
		for _, re := range dropPatterns {
			if re.MatchString(line) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		filtered = append(filtered, trailingWS.ReplaceAllString(line, ""))
	}

	// Step 2: awk-equivalent — collapse consecutive blank lines to one.
	var collapsed []string
	prevBlank := false
	for _, l := range filtered {
		if l == "" {
			if !prevBlank {
				collapsed = append(collapsed, l)
			}
			prevBlank = true
			continue
		}
		collapsed = append(collapsed, l)
		prevBlank = false
	}
	return strings.Join(collapsed, "\n")
}

// sqlcCompileWithProbe creates a throwaway probe query, runs sqlc compile,
// removes the probe, and returns any compile error. Mirrors §7 of the bash.
func sqlcCompileWithProbe(root string) (err error) {
	probeFile := filepath.Join(root, probeQuerySubdir)
	if err := os.MkdirAll(filepath.Dir(probeFile), 0o755); err != nil {
		return fmt.Errorf("mkdir probe: %w", err)
	}
	probeBody := `-- name: Phase0RenderProbe :one
-- Phase 0 EC-2 probe — DELETED at end of T0.2. Exercises sqlc's schema.gen.sql
-- parser against every existing table's DDL via SELECT 1; if any TRIGGER /
-- recursive CTE / partial index pattern cannot be parsed, sqlc compile errors
-- and this script aborts with the table identified.
SELECT 1 AS probe;
`
	if err := os.WriteFile(probeFile, []byte(probeBody), 0o644); err != nil {
		return fmt.Errorf("write probe: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(probeFile)) }()

	cfgPath := filepath.Join(root, sqlcConfigSubdir)
	cmd := exec.Command("sqlc", "compile", "--file", cfgPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
