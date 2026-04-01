// PKCE Auth helper for the dashboard.
// Uses Authorization Code Flow with PKCE against Ory Hydra.

const HYDRA_PUBLIC_URL = process.env.NEXT_PUBLIC_HYDRA_URL || 'http://localhost:4444';
const CLIENT_ID = 'loopzero-cli'; // Same client, different grant type (authorization_code with PKCE)
const REDIRECT_URI = typeof window !== 'undefined'
  ? `${window.location.origin}/login/callback`
  : 'http://localhost:3000/login/callback';

export async function startPKCEFlow(): Promise<void> {
  const verifier = generateCodeVerifier();
  const challenge = await generateCodeChallenge(verifier);

  // Store verifier for the callback
  sessionStorage.setItem('pkce_verifier', verifier);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: CLIENT_ID,
    redirect_uri: REDIRECT_URI,
    scope: 'openid offline_access',
    code_challenge: challenge,
    code_challenge_method: 'S256',
    state: crypto.randomUUID(),
  });

  window.location.href = `${HYDRA_PUBLIC_URL}/oauth2/auth?${params}`;
}

export async function exchangeCode(code: string): Promise<{ access_token: string; refresh_token: string } | null> {
  const verifier = sessionStorage.getItem('pkce_verifier');
  if (!verifier) return null;
  sessionStorage.removeItem('pkce_verifier');

  const resp = await fetch(`${HYDRA_PUBLIC_URL}/oauth2/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code',
      code,
      redirect_uri: REDIRECT_URI,
      client_id: CLIENT_ID,
      code_verifier: verifier,
    }),
  });

  if (!resp.ok) return null;
  return await resp.json();
}

function generateCodeVerifier(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return base64URLEncode(array);
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const digest = await crypto.subtle.digest('SHA-256', data);
  return base64URLEncode(new Uint8Array(digest));
}

function base64URLEncode(buffer: Uint8Array): string {
  const str = btoa(String.fromCharCode(...buffer));
  return str.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
