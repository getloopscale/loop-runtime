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

import { spawnSync } from 'child_process';
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
});
