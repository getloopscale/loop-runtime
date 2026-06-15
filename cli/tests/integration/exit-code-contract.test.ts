// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// T1.2 (CHG-2026-04-29-DOGFOOD) — Exit code contract test fork-based.
//
// **What this test enforces (Rule 4 — exit codes are permanent):**
// Para cada comando CLI que aceita `--json`, quando o comando emite um
// erro typed (LoopZeroError com `exit_code`), o **processo Node real** DEVE
// terminar com `process.exit_status === parseInt(json.exit_code)`.
//
// **Por que `child_process.spawnSync` em vez de chamar Command.execute
// diretamente?** Porque o gap original do F-02 não estava no objeto
// Command — estava no plumbing entre `Command.execute()` (que retorna
// number) e `program.parseAsync(...).then(...).catch(...)` no
// `cli/bin/loopzero`. Esse gap só é exercitado pela invocação real do
// processo. Tests unitários que chamam `cmd.execute()` diretamente
// passariam mesmo com o bug presente.
//
// **Matrix design:**
// Cada entry é `{ args, expected_min_exit, expected_codes }`. O test
// roda `node bin/loopzero <args> --json` com `LOOPZERO_URL=http://localhost:1`
// (port 1 é reserved/unreachable — provoca AUTH/SERVICE_UNAVAILABLE
// rapidamente sem precisar de cluster k3d up).

import { spawn, spawnSync } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';

const BIN_PATH = path.resolve(__dirname, '../../bin/loopzero');

interface ContractCase {
  name: string;
  args: string[];
  // Quando rodando sem auth + API offline, qual é o erro esperado?
  // Diferentes comandos têm gates diferentes (auth check vs config check).
  expectedCodes: number[];
}

// Configurations:
// - sem ~/.loopzero/config.json válido
// - LOOPZERO_URL apontando para port unreachable
// - cwd = tmp dir SEM loopzero.yaml (provoca MISSING_CONFIG em alguns) ou COM
//   loopzero.yaml (puro auth gate)
//
// Cada comando é avaliado no contexto onde produz o erro mais limpo.
const cases: ContractCase[] = [
  // === Auth-only gate (sem loopzero.yaml requirement) ===
  { name: 'whoami', args: ['whoami'], expectedCodes: [4] },
  { name: 'project list', args: ['project', 'list'], expectedCodes: [4] },
  { name: 'team list', args: ['team', 'list'], expectedCodes: [4] },
  { name: 'env list', args: ['env', 'list'], expectedCodes: [4] },

  // === Validate (config-only — não requer auth) ===
  { name: 'validate (no loopzero.yaml)', args: ['validate'], expectedCodes: [1] },

  // === Auth-required + loopzero.yaml-required ===
  { name: 'deploy (no auth)', args: ['deploy'], expectedCodes: [4] },
  { name: 'status (no auth)', args: ['status'], expectedCodes: [4] },
  { name: 'logs (no auth)', args: ['logs'], expectedCodes: [4] },
  { name: 'build list (no auth)', args: ['build', 'list'], expectedCodes: [4] },
  { name: 'secret list (no auth)', args: ['secret', 'list'], expectedCodes: [4] },
  { name: 'rollback (no auth)', args: ['rollback', '--yes'], expectedCodes: [4] },
  { name: 'cancel (no auth)', args: ['cancel', 'fake-build-id'], expectedCodes: [4] },

  // === Usage errors (sem args / flag inexistente) ===
  { name: 'unknown command', args: ['this-command-does-not-exist'], expectedCodes: [1] },
  { name: 'unknown flag', args: ['deploy', '--bogus-flag'], expectedCodes: [1] },
];

describe('CLI exit code contract (T1.2 / F-02)', () => {
  let tmpDir: string;
  let tmpHome: string;

  beforeAll(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-contract-'));
    tmpHome = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-home-'));
  });

  afterAll(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
    fs.rmSync(tmpHome, { recursive: true, force: true });
  });

  describe.each(cases)('$name', ({ args, expectedCodes }) => {
    it('process exit_status matches json exit_code in --json mode', () => {
      const result = spawnSync('node', [BIN_PATH, ...args, '--json'], {
        cwd: tmpDir,
        env: {
          ...process.env,
          LOOPZERO_URL: 'http://127.0.0.1:1', // unreachable
          HOME: tmpHome,                   // no ~/.loopzero/config.json
          XDG_CONFIG_HOME: tmpHome,
          LOOPZERO_TOKEN: '',                  // explicit clear
        },
        encoding: 'utf-8',
        timeout: 30_000,
      });

      const stdout = result.stdout || '';
      const status = result.status;

      // Algumas linhas de stdout podem ser progress messages antes do JSON.
      // Filtra a primeira linha que parsea como JSON.
      let parsed: { exit_code?: number; code?: string } | null = null;
      for (const line of stdout.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed.startsWith('{')) continue;
        try {
          parsed = JSON.parse(trimmed);
          if (parsed && (parsed.exit_code !== undefined || parsed.code !== undefined)) break;
        } catch {
          // try next line
        }
      }

      // Diagnóstico em failure: imprime tudo.
      const ctx = JSON.stringify({
        args, status, expectedCodes,
        stdout: stdout.slice(0, 500),
        stderr: (result.stderr || '').slice(0, 200),
      });

      expect(status).not.toBeNull();
      expect(expectedCodes).toContain(status as number);

      // Se houve JSON com exit_code, ele DEVE casar com o process status.
      // Não exigimos JSON em todos os casos (Commander parse errors emitem
      // texto humano em alguns paths) — mas quando há, deve ser consistente.
      if (parsed && parsed.exit_code !== undefined) {
        if (status !== parsed.exit_code) {
          throw new Error(`exit code drift: process=${status}, json=${parsed.exit_code}; ctx=${ctx}`);
        }
      }
    });
  });

  // Help / version DEVEM retornar exit 0 (não são erros).
  describe('help / version are not errors', () => {
    it('--help → exit 0', () => {
      const result = spawnSync('node', [BIN_PATH, '--help'], {
        env: { ...process.env, HOME: tmpHome, XDG_CONFIG_HOME: tmpHome },
        encoding: 'utf-8',
        timeout: 5_000,
      });
      expect(result.status).toBe(0);
      expect(result.stdout).toContain('Usage: loopzero');
    });

    it('--version → exit 0', () => {
      const result = spawnSync('node', [BIN_PATH, '--version'], {
        env: { ...process.env, HOME: tmpHome, XDG_CONFIG_HOME: tmpHome },
        encoding: 'utf-8',
        timeout: 5_000,
      });
      expect(result.status).toBe(0);
      expect(result.stdout.trim()).toMatch(/^\d+\.\d+\.\d+$/);
    });

    it('subcommand --help → exit 0', () => {
      const result = spawnSync('node', [BIN_PATH, 'deploy', '--help'], {
        env: { ...process.env, HOME: tmpHome, XDG_CONFIG_HOME: tmpHome },
        encoding: 'utf-8',
        timeout: 5_000,
      });
      expect(result.status).toBe(0);
      expect(result.stdout).toContain('Usage: loopzero deploy');
    });
  });

  // T0.3 (plan 2026-06-15-code-review-49-findings-remediation, R3-F6 HIGH)
  // — Rule 4 exit codes are permanent {0,1,2,3,4,5}. SIGINT/SIGTERM are
  // sent by the OS to the foreground process; our signal handlers in
  // `cli/src/index.ts:57-58` call `runCleanup()` then `process.exit(1)`.
  // Before the T0.2 fix the handlers used `exit(130)` (SIGINT) and
  // `exit(143)` (SIGTERM) — values OUTSIDE the canonical set. This test
  // catches that regression class by spawning the CLI in a known-hang
  // shape, sending the signal, and asserting the resulting exit code is
  // in the canonical set.
  //
  // We use `child_process.spawn` (async) rather than `spawnSync` because
  // signal-driven termination is inherently asynchronous: the parent
  // must observe `exit` to know the code. `loopzero logs --follow` against
  // an unreachable backend hangs in the SSE connect path long enough
  // for us to deliver the signal cleanly.
  describe('signal-driven exits honor Rule 4 (no exit outside {0..5})', () => {
    const CANONICAL_EXITS = [0, 1, 2, 3, 4, 5];

    interface SignalCase {
      name: string;
      signal: NodeJS.Signals;
    }

    const signalCases: SignalCase[] = [
      { name: 'SIGINT (Ctrl-C)', signal: 'SIGINT' },
      { name: 'SIGTERM (kill default)', signal: 'SIGTERM' },
    ];

    signalCases.forEach(({ name, signal }) => {
      it(`${name} → exit code ∈ {0,1,2,3,4,5}`, (done) => {
        // Spawn a long-running CLI invocation. `logs --follow` opens
        // an SSE stream + retries on EOF; against LOOPZERO_URL=127.0.0.1:1
        // (unreachable) it will spin in connect-retry without auth.
        const child = spawn('node', [BIN_PATH, 'logs', '--follow'], {
          env: {
            ...process.env,
            LOOPZERO_URL: 'http://127.0.0.1:1',
            HOME: tmpHome,
            XDG_CONFIG_HOME: tmpHome,
            LOOPZERO_TOKEN: '',
          },
          stdio: ['ignore', 'pipe', 'pipe'],
        });

        let exited = false;
        const cleanup = setTimeout(() => {
          if (!exited) {
            child.kill('SIGKILL');
            done(new Error('child did not exit within 8s after signal'));
          }
        }, 8_000);

        child.on('exit', (code, killedBySignal) => {
          exited = true;
          clearTimeout(cleanup);

          // After T0.2 fix: handler calls process.exit(1), code=1.
          // Without our handler, Node would exit with 128+signum
          // (130 for SIGINT, 143 for SIGTERM) — which would fail this
          // assertion. The test thus locks the Rule 4 contract.
          if (code === null) {
            // Killed by signal without our handler running.
            // This is the regression-defense failure case.
            done(new Error(
              `process killed by signal ${killedBySignal} without exit code; ` +
              `Rule 4 requires explicit process.exit(N) with N ∈ {0..5}. ` +
              `Check cli/src/index.ts SIGINT/SIGTERM handlers.`,
            ));
            return;
          }

          expect(CANONICAL_EXITS).toContain(code);
          done();
        });

        // Give the child a moment to start its event loop + start the
        // SSE connect-retry; THEN deliver the signal.
        setTimeout(() => {
          child.kill(signal);
        }, 250);
      }, 10_000);
    });
  });
});
