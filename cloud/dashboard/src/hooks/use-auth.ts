import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { isAuthenticated } from '@/lib/api';

// Redirects to /login if not authenticated.
// Returns false during redirect to prevent content flash.
export function useRequireAuth(): boolean {
  const navigate = useNavigate();
  const authed = isAuthenticated();

  useEffect(() => {
    if (!authed) {
      navigate('/login', { replace: true });
    }
  }, [authed, navigate]);

  return authed;
}
