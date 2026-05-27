import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { matchRoute, registerRoute, resetRoutes, getRegisteredRouteCount } from './server';

describe('mocks/server — route registry', () => {
  beforeEach(() => {
    resetRoutes();
  });

  afterEach(() => {
    resetRoutes();
  });

  it('test_starts_with_zero_routes', () => {
    expect(getRegisteredRouteCount()).toBe(0);
  });

  it('test_registers_static_path', async () => {
    registerRoute('GET', '/api/v1/projects', async () =>
      Response.json({ projects: [] }),
    );
    expect(getRegisteredRouteCount()).toBe(1);
    const matched = matchRoute('GET', '/api/v1/projects');
    expect(matched).not.toBeNull();
    expect(matched!.params).toEqual({});
  });

  it('test_extracts_single_path_param', async () => {
    registerRoute('GET', '/api/v1/projects/{name}', async () =>
      Response.json({}),
    );
    const matched = matchRoute('GET', '/api/v1/projects/acme-web');
    expect(matched).not.toBeNull();
    expect(matched!.params).toEqual({ name: 'acme-web' });
  });

  it('test_extracts_multiple_path_params', async () => {
    registerRoute(
      'DELETE',
      '/api/v1/projects/{name}/environments/{env}',
      async () => new Response(null, { status: 204 }),
    );
    const matched = matchRoute(
      'DELETE',
      '/api/v1/projects/acme-web/environments/production',
    );
    expect(matched).not.toBeNull();
    expect(matched!.params).toEqual({ name: 'acme-web', env: 'production' });
  });

  it('test_decodes_url_encoded_params', async () => {
    registerRoute('GET', '/api/v1/domains/{domain}', async () =>
      Response.json({}),
    );
    const matched = matchRoute('GET', '/api/v1/domains/app.acme.com');
    expect(matched!.params.domain).toBe('app.acme.com');
  });

  it('test_returns_null_for_unmatched_path', () => {
    registerRoute('GET', '/api/v1/projects', async () => Response.json({}));
    const matched = matchRoute('GET', '/api/v1/unknown');
    expect(matched).toBeNull();
  });

  it('test_returns_null_when_method_mismatched', () => {
    registerRoute('GET', '/api/v1/projects', async () => Response.json({}));
    const matched = matchRoute('POST', '/api/v1/projects');
    expect(matched).toBeNull();
  });

  it('test_does_not_match_partial_path', () => {
    registerRoute('GET', '/api/v1/projects', async () => Response.json({}));
    const matched = matchRoute('GET', '/api/v1/projects/extra');
    expect(matched).toBeNull();
  });

  it('test_param_does_not_match_slash', () => {
    // {name} must NOT match a path with embedded slashes (would break nested routes)
    registerRoute('GET', '/api/v1/projects/{name}', async () =>
      Response.json({}),
    );
    const matched = matchRoute('GET', '/api/v1/projects/foo/bar');
    expect(matched).toBeNull();
  });

  it('test_supports_all_http_methods', () => {
    registerRoute('GET', '/api/v1/r', async () => Response.json({}));
    registerRoute('POST', '/api/v1/r', async () => Response.json({}));
    registerRoute('PUT', '/api/v1/r', async () => Response.json({}));
    registerRoute('PATCH', '/api/v1/r', async () => Response.json({}));
    registerRoute('DELETE', '/api/v1/r', async () => Response.json({}));
    expect(matchRoute('GET', '/api/v1/r')).not.toBeNull();
    expect(matchRoute('POST', '/api/v1/r')).not.toBeNull();
    expect(matchRoute('PUT', '/api/v1/r')).not.toBeNull();
    expect(matchRoute('PATCH', '/api/v1/r')).not.toBeNull();
    expect(matchRoute('DELETE', '/api/v1/r')).not.toBeNull();
  });

  it('test_handler_receives_params_object', async () => {
    let receivedParams: Record<string, string> = {};
    registerRoute('GET', '/api/v1/projects/{name}', async (_req, params) => {
      receivedParams = params;
      return Response.json({});
    });
    const matched = matchRoute('GET', '/api/v1/projects/test-app');
    await matched!.route.handler(new Request('https://test/x'), matched!.params);
    expect(receivedParams).toEqual({ name: 'test-app' });
  });

  it('test_handler_can_return_status_codes', async () => {
    registerRoute('POST', '/api/v1/projects', async () =>
      Response.json({ code: 'CONFLICT', message: 'taken' }, { status: 409 }),
    );
    const matched = matchRoute('POST', '/api/v1/projects');
    const res = await matched!.route.handler(
      new Request('https://test/x', { method: 'POST' }),
      {},
    );
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.code).toBe('CONFLICT');
  });
});
