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
