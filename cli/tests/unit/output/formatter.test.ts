import { Formatter } from '../../../src/output/formatter';

describe('Formatter', () => {
  let stdout: string[];
  let stderr: string[];
  let formatter: Formatter;

  beforeEach(() => {
    stdout = [];
    stderr = [];
    formatter = new Formatter({
      writeTo: (msg: string) => stdout.push(msg),
      writeError: (msg: string) => stderr.push(msg),
      isCI: false,
      isJSON: false,
    });
  });

  describe('success', () => {
    it('should write success message to stdout', () => {
      formatter.success('Deploy complete');
      expect(stdout.length).toBe(1);
      expect(stdout[0]).toContain('Deploy complete');
    });
  });

  describe('error', () => {
    it('should write error message to stderr', () => {
      formatter.error('Something failed');
      expect(stderr.length).toBe(1);
      expect(stderr[0]).toContain('Something failed');
    });

    it('should not contain forbidden internal terms', () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];
      formatter.error('Deploy failed for service api');
      const output = stderr.join('');
      for (const term of forbidden) {
        expect(output).not.toContain(term);
      }
    });
  });

  describe('info', () => {
    it('should write info message to stdout', () => {
      formatter.info('Checking project...');
      expect(stdout.length).toBe(1);
      expect(stdout[0]).toContain('Checking project...');
    });
  });

  describe('json', () => {
    it('should output valid JSON to stdout', () => {
      const jsonFormatter = new Formatter({
        writeTo: (msg: string) => stdout.push(msg),
        writeError: (msg: string) => stderr.push(msg),
        isCI: false,
        isJSON: true,
      });
      jsonFormatter.json({ status: 'success', deploy_id: 'd-8f3a' });
      expect(stdout.length).toBe(1);
      const parsed = JSON.parse(stdout[0]);
      expect(parsed.status).toBe('success');
      expect(parsed.deploy_id).toBe('d-8f3a');
    });
  });

  describe('table', () => {
    it('should output formatted table to stdout', () => {
      formatter.table(['Name', 'Status'], [
        ['api', 'running'],
        ['web', 'running'],
      ]);
      expect(stdout.length).toBeGreaterThan(0);
      const output = stdout.join('\n');
      expect(output).toContain('Name');
      expect(output).toContain('Status');
      expect(output).toContain('api');
      expect(output).toContain('web');
    });
  });

  describe('CI mode', () => {
    it('should not include ANSI color codes in CI mode', () => {
      const ciFormatter = new Formatter({
        writeTo: (msg: string) => stdout.push(msg),
        writeError: (msg: string) => stderr.push(msg),
        isCI: true,
        isJSON: false,
      });
      ciFormatter.success('Deploy complete');
      ciFormatter.error('Build failed');
      const all = [...stdout, ...stderr].join('');
      expect(all).not.toMatch(/\x1b\[/);
    });
  });

  describe('CI detection', () => {
    const originalEnv = process.env;

    afterEach(() => {
      process.env = originalEnv;
    });

    it('should detect CI=true', () => {
      process.env = { ...originalEnv, CI: 'true' };
      expect(Formatter.detectCI()).toBe(true);
    });

    it('should detect GITHUB_ACTIONS', () => {
      process.env = { ...originalEnv, GITHUB_ACTIONS: 'true' };
      expect(Formatter.detectCI()).toBe(true);
    });

    it('should detect GITLAB_CI', () => {
      process.env = { ...originalEnv, GITLAB_CI: 'true' };
      expect(Formatter.detectCI()).toBe(true);
    });

    it('should detect CIRCLECI', () => {
      process.env = { ...originalEnv, CIRCLECI: 'true' };
      expect(Formatter.detectCI()).toBe(true);
    });

    it('should detect JENKINS_URL', () => {
      process.env = { ...originalEnv, JENKINS_URL: 'http://jenkins.local' };
      expect(Formatter.detectCI()).toBe(true);
    });

    it('should return false when no CI env vars', () => {
      process.env = {};
      expect(Formatter.detectCI()).toBe(false);
    });
  });

  describe('exit codes', () => {
    it('should only allow valid exit codes 0-5', () => {
      const valid = [0, 1, 2, 3, 4, 5];
      for (const code of valid) {
        expect(Formatter.isValidExitCode(code)).toBe(true);
      }
    });

    it('should reject invalid exit codes', () => {
      const invalid = [-1, 6, 127, 255];
      for (const code of invalid) {
        expect(Formatter.isValidExitCode(code)).toBe(false);
      }
    });
  });
});
