import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getSession, type Session } from '@/lib/auth';

/** Check Kratos session. Redirects to /login if not authenticated. */
export function useRequireAuth(): Session | null {
  const navigate = useNavigate();
  const [session, setSession] = useState<Session | null>(null);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    getSession().then(s => {
      setSession(s);
      setChecked(true);
      if (!s) {
        navigate('/login', { replace: true });
      }
    });
  }, [navigate]);

  if (!checked) return null; // Still loading
  return session;
}
