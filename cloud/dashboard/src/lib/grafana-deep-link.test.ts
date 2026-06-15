/**
 * grafana-deep-link.test.ts — vitest hermetic tests for T2.3.
 *
 * Covers:
 *  - happy path: base URL + project + env + signal → templated URL
 *  - EC-6: project name with special chars (`&`, `?`, `#`) → URL-encoded
 *  - EC-13: grafanaBaseURL empty/null → returns null (honest disabled)
 *  - edge: trailing slash on baseURL → stripped to avoid double-slash
 *  - edge: signal omitted → URL without var-signal param
 */

import { describe, it, expect } from 'vitest';

import { buildGrafanaDeepLink } from './grafana-deep-link';

describe('buildGrafanaDeepLink — T2.3 J-18', () => {
  it('templates URL with all params when grafanaBaseURL set', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'teste',
      env: 'production',
      signal: 'uptime_24h',
    });

    expect(url).toBe(
      'https://grafana.loopzero.local/d/loopzero-project?var-project=teste&var-env=production&var-signal=uptime_24h',
    );
  });

  it('EC-6: URL-encodes project name with special chars (&, ?, #)', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'proj&test?env#prod',
      env: 'production',
      signal: 'uptime_24h',
    });

    // URLSearchParams encodes & as %26, ? as %3F, # as %23
    expect(url).toContain('var-project=proj%26test%3Fenv%23prod');
    // Defense: the URL must still be parsable
    expect(() => new URL(url!)).not.toThrow();
  });

  it('EC-6: URL-encodes env name with spaces and accents', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'teste',
      env: 'prod ção',
      signal: 'uptime_24h',
    });

    // Space encoded as + by URLSearchParams; ção encoded
    expect(url).toContain('var-env=prod+%C3%A7%C3%A3o');
  });

  it('EC-13: returns null when grafanaBaseURL is empty string (honest disabled)', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: '',
      projectName: 'teste',
      env: 'production',
      signal: 'uptime_24h',
    });

    expect(url).toBeNull();
  });

  it('EC-13: returns null when grafanaBaseURL is null', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: null,
      projectName: 'teste',
      env: 'production',
    });

    expect(url).toBeNull();
  });

  it('EC-13: returns null when grafanaBaseURL is undefined', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: undefined,
      projectName: 'teste',
      env: 'production',
    });

    expect(url).toBeNull();
  });

  it('EC-13: returns null when grafanaBaseURL is whitespace-only', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: '   ',
      projectName: 'teste',
      env: 'production',
    });

    expect(url).toBeNull();
  });

  it('strips trailing slash from baseURL to avoid double-slash', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local/',
      projectName: 'teste',
      env: 'production',
      signal: 'uptime_24h',
    });

    // Must not contain `local//d/`
    expect(url).not.toContain('local//d/');
    expect(url).toContain('local/d/loopzero-project');
  });

  it('strips multiple trailing slashes', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local///',
      projectName: 'teste',
      env: 'production',
    });

    expect(url).toContain('local/d/loopzero-project');
    expect(url).not.toContain('local//d/');
  });

  it('omits var-signal param when signal not provided', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'teste',
      env: 'production',
    });

    expect(url).toBe('https://grafana.loopzero.local/d/loopzero-project?var-project=teste&var-env=production');
    expect(url).not.toContain('var-signal');
  });

  it('omits var-signal param when signal is empty string', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'teste',
      env: 'production',
      signal: '',
    });

    expect(url).not.toContain('var-signal');
  });

  it('omits var-signal when signal is whitespace-only', () => {
    const url = buildGrafanaDeepLink({
      grafanaBaseURL: 'https://grafana.loopzero.local',
      projectName: 'teste',
      env: 'production',
      signal: '   ',
    });

    expect(url).not.toContain('var-signal');
  });
});
