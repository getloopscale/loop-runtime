import { DeployRenderer, DeployRendererOptions, DeployEvent, DeployResult, AppResult } from '../../../src/output/deploy-renderer';

function createRenderer(overrides: Partial<DeployRendererOptions> = {}): DeployRenderer {
  return new DeployRenderer({
    isCI: false,
    isJSON: false,
    isTTY: true,
    ...overrides,
  });
}

function makeDeployResult(overrides: Partial<DeployResult> = {}): DeployResult {
  return {
    deploy_id: 'd-8f3a',
    status: 'success',
    duration_seconds: 97,
    apps: [
      { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
      { name: 'web', status: 'deployed', url: 'https://web.example.com', build_duration_seconds: 52 },
    ],
    ...overrides,
  };
}

const FORBIDDEN_TERMS = [
  'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
  'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
  'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
];

describe('DeployRenderer', () => {
  describe('TTY mode', () => {
    it('should render building event with spinner character', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'build.started',
        data: { app: 'api' },
      });

      expect(output).toBeDefined();
      expect(output).toContain('Building api');
    });

    it('should render complete event with checkmark and duration', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'build.completed',
        data: { app: 'api', duration_seconds: 45 },
      });

      expect(output).toBeDefined();
      expect(output).toContain('api');
      expect(output).toContain('45s');
      expect(output).toMatch(/✓/);
    });

    it('should render failed event with X mark and error message', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'build.failed',
        data: { app: 'api', error: 'compilation error' },
      });

      expect(output).toBeDefined();
      expect(output).toContain('api');
      expect(output).toContain('compilation error');
      expect(output).toMatch(/✗/);
    });

    it('should display URLs on success result', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });
      const result = makeDeployResult();

      const output = renderer.renderResult(result);

      expect(output).toContain('https://api.example.com');
      expect(output).toContain('https://web.example.com');
      expect(output).toContain('api');
      expect(output).toContain('web');
    });

    it('should show "(no URL — background worker)" for worker apps without URL', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });
      const result = makeDeployResult({
        apps: [
          { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
          { name: 'worker', status: 'deployed', build_duration_seconds: 30 },
        ],
      });

      const output = renderer.renderResult(result);

      expect(output).toContain('(no URL — background worker)');
      expect(output).toContain('worker');
    });

    it('should render per-app building lines when apps[] present in build.started (F-01)', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'build.started',
        data: { apps: ['api', 'web', 'worker'] },
      });

      expect(output).toBeDefined();
      expect(output).toContain('Building api');
      expect(output).toContain('Building web');
      expect(output).toContain('Building worker');
    });

    it('should render health check event', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'health.checking',
        data: { app: 'api' },
      });

      expect(output).toBeDefined();
      expect(output).toContain('health');
    });

    it('should render health check complete event with duration', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const output = renderer.onEvent({
        type: 'health.passed',
        data: { app: 'api', duration_seconds: 8 },
      });

      expect(output).toBeDefined();
      expect(output).toContain('8s');
      expect(output).toMatch(/✓/);
    });
  });

  describe('CI mode', () => {
    it('should render building event as plain text with [build] prefix', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const output = renderer.onEvent({
        type: 'build.started',
        data: { app: 'api' },
      });

      expect(output).toBe('[build] api: building...');
    });

    it('should render per-app building lines in CI mode when apps[] present (F-01)', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const output = renderer.onEvent({
        type: 'build.started',
        data: { apps: ['api', 'gateway'] },
      });

      expect(output).toBeDefined();
      expect(output).toContain('[build] api: building...');
      expect(output).toContain('[build] gateway: building...');
    });

    it('should render build complete with duration and [build] prefix', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const output = renderer.onEvent({
        type: 'build.completed',
        data: { app: 'api', duration_seconds: 45 },
      });

      expect(output).toBe('[build] api: complete [45s]');
    });

    it('should render deployed event with URL and [deploy] prefix', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const output = renderer.onEvent({
        type: 'deploy.app_live',
        data: { app: 'api', url: 'https://api.example.com' },
      });

      expect(output).toBe('[deploy] api: deployed https://api.example.com');
    });

    it('should not contain colors, emojis, or spinner characters', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const events: DeployEvent[] = [
        { type: 'build.started', data: { app: 'api' } },
        { type: 'build.completed', data: { app: 'api', duration_seconds: 45 } },
        { type: 'build.failed', data: { app: 'web', error: 'timeout' } },
        { type: 'deploy.app_live', data: { app: 'api', url: 'https://api.example.com' } },
        { type: 'health.checking', data: { app: 'api' } },
        { type: 'health.passed', data: { app: 'api', duration_seconds: 8 } },
      ];

      const spinnerChars = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
      const emojiPattern = /[\u{1F600}-\u{1F64F}\u{1F680}-\u{1F6FF}\u{1F900}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
      const ansiPattern = /\x1b\[/;

      for (const event of events) {
        const output = renderer.onEvent(event);
        if (output) {
          for (const sc of spinnerChars) {
            expect(output).not.toContain(sc);
          }
          expect(output).not.toMatch(emojiPattern);
          expect(output).not.toMatch(ansiPattern);
        }
      }

      const result = makeDeployResult();
      const resultOutput = renderer.renderResult(result);
      for (const sc of spinnerChars) {
        expect(resultOutput).not.toContain(sc);
      }
      expect(resultOutput).not.toMatch(emojiPattern);
      expect(resultOutput).not.toMatch(ansiPattern);
    });

    it('should render worker apps with "(no URL — background worker)" in CI mode', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });
      const result = makeDeployResult({
        apps: [
          { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
          { name: 'worker', status: 'deployed', build_duration_seconds: 30 },
        ],
      });

      const output = renderer.renderResult(result);

      expect(output).toContain('(no URL — background worker)');
    });
  });

  describe('JSON mode', () => {
    it('should output valid JSON matching DA-5 schema', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult();

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      expect(parsed).toHaveProperty('deploy_id');
      expect(parsed).toHaveProperty('status');
      expect(parsed).toHaveProperty('duration_seconds');
      expect(parsed).toHaveProperty('apps');
      expect(Array.isArray(parsed.apps)).toBe(true);

      const app = parsed.apps[0];
      expect(app).toHaveProperty('name');
      expect(app).toHaveProperty('status');
      expect(app).toHaveProperty('url');
      expect(app).toHaveProperty('build_duration_seconds');
    });

    it('should have deploy_id matching d-{4hex} format (DA-11)', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult({ deploy_id: 'd-8f3a' });

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      expect(parsed.deploy_id).toMatch(/^d-[0-9a-f]{4}$/);
    });

    it('should include error field for failed apps', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult({
        status: 'partial_failure',
        apps: [
          { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 30 },
          { name: 'web', status: 'failed', error: 'Compilation error', build_duration_seconds: 30 },
        ],
      });

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      const failedApp = parsed.apps.find((a: AppResult) => a.status === 'failed');
      expect(failedApp).toHaveProperty('error', 'Compilation error');
    });

    it('should suppress intermediate event output in JSON mode', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });

      const output = renderer.onEvent({
        type: 'build.started',
        data: { app: 'api' },
      });

      expect(output).toBeNull();
    });

    it('should output exactly one JSON object (no extra text)', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult();

      const output = renderer.renderJSON(result);

      // Should parse as valid JSON without extra text
      expect(() => JSON.parse(output)).not.toThrow();
      // Should be exactly one JSON object (starts with { ends with })
      expect(output.trim()).toMatch(/^\{[\s\S]*\}$/);
    });
  });

  describe('worker app URL normalization in JSON (F-08)', () => {
    it('should output url as null for apps with empty string URL', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult({
        apps: [
          { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
          { name: 'worker', status: 'deployed', url: '', build_duration_seconds: 30 },
        ],
      });

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      const workerApp = parsed.apps.find((a: AppResult) => a.name === 'worker');
      expect(workerApp.url).toBeNull();
    });

    it('should output url as null for apps with undefined URL', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult({
        apps: [
          { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
          { name: 'worker', status: 'deployed', build_duration_seconds: 30 },
        ],
      });

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      const workerApp = parsed.apps.find((a: AppResult) => a.name === 'worker');
      expect(workerApp.url).toBeNull();
    });

    it('should preserve valid URLs in JSON output', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult();

      const output = renderer.renderJSON(result);
      const parsed = JSON.parse(output);

      const apiApp = parsed.apps.find((a: AppResult) => a.name === 'api');
      expect(apiApp.url).toBe('https://api.example.com');
    });
  });

  describe('no internal terms (Rule 3)', () => {
    it('should not contain forbidden internal terms in TTY output', () => {
      const renderer = createRenderer({ isTTY: true, isCI: false, isJSON: false });

      const events: DeployEvent[] = [
        { type: 'build.started', data: { app: 'api' } },
        { type: 'build.completed', data: { app: 'api', duration_seconds: 45 } },
        { type: 'build.failed', data: { app: 'web', error: 'timeout' } },
        { type: 'deploy.app_live', data: { app: 'api', url: 'https://api.example.com' } },
        { type: 'health.checking', data: { app: 'api' } },
        { type: 'health.passed', data: { app: 'api', duration_seconds: 8 } },
      ];

      const allOutputs: string[] = [];
      for (const event of events) {
        const output = renderer.onEvent(event);
        if (output) allOutputs.push(output);
      }

      const result = makeDeployResult();
      allOutputs.push(renderer.renderResult(result));

      const combined = allOutputs.join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(combined).not.toContain(term);
      }
    });

    it('should not contain forbidden internal terms in CI output', () => {
      const renderer = createRenderer({ isCI: true, isJSON: false, isTTY: false });

      const events: DeployEvent[] = [
        { type: 'build.started', data: { app: 'api' } },
        { type: 'build.completed', data: { app: 'api', duration_seconds: 45 } },
        { type: 'build.failed', data: { app: 'web', error: 'timeout' } },
        { type: 'deploy.app_live', data: { app: 'api', url: 'https://api.example.com' } },
      ];

      const allOutputs: string[] = [];
      for (const event of events) {
        const output = renderer.onEvent(event);
        if (output) allOutputs.push(output);
      }

      const result = makeDeployResult();
      allOutputs.push(renderer.renderResult(result));

      const combined = allOutputs.join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(combined).not.toContain(term);
      }
    });

    it('should not contain forbidden internal terms in JSON output', () => {
      const renderer = createRenderer({ isJSON: true, isCI: false, isTTY: false });
      const result = makeDeployResult();

      const output = renderer.renderJSON(result);

      for (const term of FORBIDDEN_TERMS) {
        expect(output).not.toContain(term);
      }
    });
  });
});
