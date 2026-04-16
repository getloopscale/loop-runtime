import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getSession, type Session } from '@/lib/auth';
import { exchangeSessionToken, hasValidToken } from '@/lib/api';

/** Check Kratos session + exchange for JWT. Redirects to /login if not authenticated. */
export function useRequireAuth(): Session | null {
  const navigate = useNavigate();
  const [session, setSession] = useState<Session | null>(null);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    async function init() {
      const s = await getSession();
      if (!s) {
        navigate('/login', { replace: true });
        setChecked(true);
        return;
      }
      setSession(s);

      // Exchange Kratos session for JWT (BFF token exchange)
      if (!hasValidToken()) {
        await exchangeSessionToken();
      }

      setChecked(true);
    }
    init();
  }, [navigate]);

  if (!checked) return null; // Still loading
  return session;
}
