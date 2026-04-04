// PKCE Auth flow for the LoopZero dashboard.
// Uses Authorization Code Flow with PKCE against Ory Hydra.

import { getApiBase, setAuth } from './api';

const AUTH_URL = import.meta.env.VITE_AUTH_URL || 'http://localhost:4444';
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

  window.location.href = `${AUTH_URL}/oauth2/auth?${params}`;
}

// Handle callback — exchange code for tokens
export async function handleCallback(code: string, state: string): Promise<boolean> {
  const savedState = sessionStorage.getItem('pkce_state');
  const verifier = sessionStorage.getItem('pkce_verifier');

  sessionStorage.removeItem('pkce_state');
  sessionStorage.removeItem('pkce_verifier');

  if (!verifier || state !== savedState) {
    return false;
  }

  try {
    const resp = await fetch(`${AUTH_URL}/oauth2/token`, {
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

    if (!resp.ok) return false;

    const data = await resp.json();
    if (!data.access_token) return false;

    // Get user info from cloud API
    const meResp = await fetch(`${getApiBase()}/v1/auth/me`, {
      headers: { Authorization: `Bearer ${data.access_token}` },
    });
    const me = meResp.ok ? await meResp.json() : { email: '' };

    setAuth(data.access_token, data.refresh_token || '', me.email || '');
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
