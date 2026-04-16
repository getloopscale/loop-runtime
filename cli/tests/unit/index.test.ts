// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import * as path from 'path';
import { program, createProgram } from '../../src/index';

describe('bin/loopzero entrypoint', () => {
  const binPath = path.resolve(__dirname, '../../bin/loopzero');

  it('should produce help output when invoked with --help', () => {
    const output = execSync(`node ${binPath} --help`, { encoding: 'utf-8' });
    expect(output).toContain('loopzero');
    expect(output).toContain('Commands:');
    expect(output).toContain('login');
    expect(output).toContain('deploy');
  });

  it('should produce version output when invoked with --version', () => {
    const output = execSync(`node ${binPath} --version`, { encoding: 'utf-8' });
    expect(output.trim()).toMatch(/^\d+\.\d+\.\d+$/);
  });
});

describe('CLI entrypoint', () => {
  it('should export a Commander program named "loopzero"', () => {
    expect(program.name()).toBe('loopzero');
  });

  it('should have a semver version string', () => {
    const version = program.version();
    expect(version).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it('should have --json global option', () => {
    const jsonOption = program.options.find(
      (opt) => opt.long === '--json'
    );
    expect(jsonOption).toBeDefined();
  });

  it('should export createProgram factory function', () => {
    expect(typeof createProgram).toBe('function');
  });

  describe('registered commands', () => {
    const commandNames = program.commands.map((cmd) => cmd.name());

    it('should register login command', () => {
      expect(commandNames).toContain('login');
    });

    it('should register logout command', () => {
      expect(commandNames).toContain('logout');
    });

    it('should register whoami command', () => {
      expect(commandNames).toContain('whoami');
    });

    it('should register init command', () => {
      expect(commandNames).toContain('init');
    });

    it('should register project command', () => {
      expect(commandNames).toContain('project');
    });

    it('should register project delete subcommand', () => {
      const projectCmd = program.commands.find((cmd) => cmd.name() === 'project');
      expect(projectCmd).toBeDefined();
      const subcommandNames = projectCmd!.commands.map((cmd) => cmd.name());
      expect(subcommandNames).toContain('delete');
    });
  });

  describe('login command options', () => {
    const loginCmd = program.commands.find((cmd) => cmd.name() === 'login');

    it('should have --token option', () => {
      const tokenOpt = loginCmd?.options.find(
        (opt) => opt.long === '--token'
      );
      expect(tokenOpt).toBeDefined();
    });

    it('should have --team option', () => {
      const teamOpt = loginCmd?.options.find(
        (opt) => opt.long === '--team'
      );
      expect(teamOpt).toBeDefined();
    });
  });

  describe('init command options', () => {
    const initCmd = program.commands.find((cmd) => cmd.name() === 'init');

    it('should have --yes option', () => {
      const yesOpt = initCmd?.options.find(
        (opt) => opt.long === '--yes'
      );
      expect(yesOpt).toBeDefined();
    });

    it('should have --force option', () => {
      const forceOpt = initCmd?.options.find(
        (opt) => opt.long === '--force'
      );
      expect(forceOpt).toBeDefined();
    });

    it('should have --name option', () => {
      const nameOpt = initCmd?.options.find(
        (opt) => opt.long === '--name'
      );
      expect(nameOpt).toBeDefined();
    });
  });

  describe('createProgram isolation', () => {
    it('should create independent program instances', () => {
      const p1 = createProgram();
      const p2 = createProgram();
      expect(p1).not.toBe(p2);
      expect(p1.name()).toBe('loopzero');
      expect(p2.name()).toBe('loopzero');
    });
  });
});

describe('loopzero --json with no command', () => {
  const binPath = path.resolve(__dirname, '../../bin/loopzero');

  it('should output structured JSON when --json is passed without a command', () => {
    let output: string;
    let exitCode: number;
    try {
      output = execSync(`node ${binPath} --json`, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] });
      exitCode = 0;
    } catch (err: unknown) {
      const e = err as { stdout?: string; status: number };
      output = e.stdout || '';
      exitCode = e.status;
    }

    expect(exitCode).toBe(1);
    const parsed = JSON.parse(output.trim());
    expect(parsed).toHaveProperty('error');
    expect(parsed.error).toContain('No command specified');
    expect(parsed).toHaveProperty('commands');
    expect(Array.isArray(parsed.commands)).toBe(true);
    expect(parsed.commands).toContain('login');
    expect(parsed.commands).toContain('deploy');
    expect(parsed).toHaveProperty('exit_code', 1);
  });

  it('should return exit code 1 and error message for unknown command', () => {
    let output: string;
    let exitCode: number;
    try {
      output = execSync(`node ${binPath} nonexistent-command`, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] });
      exitCode = 0;
    } catch (err: unknown) {
      const e = err as { stdout?: string; stderr?: string; status: number };
      output = (e.stderr || '') + (e.stdout || '');
      exitCode = e.status;
    }

    expect(exitCode).toBe(1);
    expect(output).toContain('Unknown command: nonexistent-command');
    expect(output).toContain('loopzero --help');
  });

  it('should return exit code 1 and structured JSON for unknown command with --json', () => {
    let output: string;
    let exitCode: number;
    try {
      output = execSync(`node ${binPath} --json nonexistent-command`, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] });
      exitCode = 0;
    } catch (err: unknown) {
      const e = err as { stdout?: string; status: number };
      output = e.stdout || '';
      exitCode = e.status;
    }

    expect(exitCode).toBe(1);
    const parsed = JSON.parse(output.trim());
    expect(parsed).toHaveProperty('error');
    expect(parsed.error).toContain('Unknown command: nonexistent-command');
    expect(parsed).toHaveProperty('exit_code', 1);
  });

  it('should return structured JSON for missing required argument with --json (F-05)', () => {
    let output: string;
    let exitCode: number;
    try {
      output = execSync(`node ${binPath} project delete --yes --json`, {
        encoding: 'utf-8',
        stdio: ['pipe', 'pipe', 'pipe'],
      });
      exitCode = 0;
    } catch (err: unknown) {
      const e = err as { stdout?: string; stderr?: string; status: number };
      output = (e.stdout || '') + (e.stderr || '');
      exitCode = e.status;
    }

    expect(exitCode).toBe(1);
    const parsed = JSON.parse(output.trim());
    expect(parsed).toHaveProperty('error');
    expect(parsed.error).toMatch(/missing|required|name/i);
    expect(parsed).toHaveProperty('exit_code', 1);
  });

  it('should still show text help when --json is NOT passed and no command is given', () => {
    let stdout = '';
    let stderr = '';
    try {
      stdout = execSync(`node ${binPath}`, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] });
    } catch (err: unknown) {
      const e = err as { stdout?: string; stderr?: string };
      stdout = e.stdout || '';
      stderr = e.stderr || '';
    }
    const combined = stdout + stderr;

    expect(combined).toContain('Usage:');
    expect(combined).toContain('Commands:');
  });
});
