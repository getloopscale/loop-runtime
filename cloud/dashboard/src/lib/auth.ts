// PKCE Auth flow for the LoopZero dashboard.
// Uses Authorization Code Flow with PKCE against Ory Hydra.

import { getApiBase, setAuth } from './api';

// Hydra endpoints:
// - Authorize: browser redirect to auth.{domain} (no CORS needed)
// - Token: POST to same origin via gateway (avoids CORS — Hydra CORS is unreliable)
const HYDRA_URL = import.meta.env.VITE_AUTH_URL || 'http://localhost:4444';
const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';
const CLIENT_ID = 'loopzero-cli';

function getRedirectUri(): string {
  return `${window.location.origin}/login/callback`;
}

// Start PKCE flow — redirect to Hydra authorization endpoint
export async function startPKCELogin(): Promise<void> {
  const verifier = generateCodeVerifier();
  const challenge = await generateCodeChallenge(verifier);
  const state = crypto.randomUUID();

  sessionStorage.setItem('pkce_verifier', verifier);
  sessionStorage.setItem('pkce_state', state);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: CLIENT_ID,
    redirect_uri: getRedirectUri(),
    scope: 'openid offline_access',
    code_challenge: challenge,
    code_challenge_method: 'S256',
    state,
  });

  // Browser redirect — goes to Hydra directly (no CORS needed for redirects)
  window.location.href = `${HYDRA_URL}/oauth2/auth?${params}`;
}

// Handle callback — exchange code for tokens
export async function handleCallback(code: string, state: string): Promise<boolean> {
  const savedState = sessionStorage.getItem('pkce_state');
  const verifier = sessionStorage.getItem('pkce_verifier');

  sessionStorage.removeItem('pkce_state');
  sessionStorage.removeItem('pkce_verifier');

  if (!verifier || state !== savedState) {
    console.error('[auth] PKCE validation failed:', { hasVerifier: !!verifier, stateMatch: state === savedState, savedState: savedState?.substring(0, 8), receivedState: state?.substring(0, 8) });
    return false;
  }

  try {
    // Token exchange via same-origin gateway (cloud.dev/oauth2/token → Hydra)
    const resp = await fetch(`${API_URL}/oauth2/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'authorization_code',
        code,
        redirect_uri: getRedirectUri(),
        client_id: CLIENT_ID,
        code_verifier: verifier,
      }),
    });

    if (!resp.ok) {
      const errBody = await resp.text().catch(() => '');
      console.error('[auth] Token exchange failed:', resp.status, errBody.substring(0, 200));
      return false;
    }

    const data = await resp.json();
    if (!data.access_token) return false;

    // Get user info from cloud API
    const meResp = await fetch(`${getApiBase()}/v1/auth/me`, {
      headers: { Authorization: `Bearer ${data.access_token}` },
    });
    const me = meResp.ok ? await meResp.json() : { email: '', name: '' };

    setAuth(data.access_token, data.refresh_token || '', me.email || '', me.name || '');
    return true;
  } catch {
    return false;
  }
}

// GitHub OAuth — redirect to Kratos social login
export function startGitHubLogin(): void {
  startPKCELogin(); // Kratos shows GitHub option in the login UI
}

// Google OAuth — redirect to Kratos social login
export function startGoogleLogin(): void {
  startPKCELogin(); // Kratos shows Google option in the login UI
}

function generateCodeVerifier(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return base64URLEncode(array);
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const data = new TextEncoder().encode(verifier);
  const digest = await crypto.subtle.digest('SHA-256', data);
  return base64URLEncode(new Uint8Array(digest));
}

function base64URLEncode(buffer: Uint8Array): string {
  const str = btoa(String.fromCharCode(...buffer));
  return str.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
