import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/button';

const ACCOUNTS_URL = import.meta.env.VITE_ACCOUNTS_URL || 'http://localhost:4433';

interface ErrorDetails {
  id: string;
  error: {
    code?: number;
    status?: string;
    message?: string;
    reason?: string;
  };
}

export function ErrorPage() {
  const [searchParams] = useSearchParams();
  const [error, setError] = useState<ErrorDetails | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const errorId = searchParams.get('id');
    if (!errorId) {
      setLoading(false);
      return;
    }
    fetch(`${ACCOUNTS_URL}/self-service/errors?id=${errorId}`, { credentials: 'include' })
      .then(r => r.json())
      .then(setError)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [searchParams]);

  // Map internal error messages to user-friendly ones (Rule 3)
  const getUserMessage = (): string => {
    if (!error?.error) return 'An unexpected error occurred.';
    const msg = error.error.message || error.error.reason || '';
    // Strip any internal terms that might leak
    if (msg.includes('csrf') || msg.includes('CSRF')) return 'Your session expired. Please try again.';
    if (msg.includes('expired')) return 'This link has expired. Please start over.';
    if (msg.includes('not found')) return 'The page you were looking for was not found.';
    return msg || 'Something went wrong. Please try again.';
  };

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center">
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40 text-center">
          <AlertTriangle className="h-12 w-12 text-warning mx-auto mb-4" />
          <h1 className="font-display text-xl font-semibold text-white mb-2">Something went wrong</h1>
          {loading ? (
            <p className="text-sm text-white/40">Loading error details...</p>
          ) : (
            <>
              <p className="text-sm text-white/40 mb-6">{getUserMessage()}</p>
              <div className="space-y-3">
                <Link to="/login">
                  <Button size="lg" className="w-full justify-center h-12">Back to login</Button>
                </Link>
                <Link to="/" className="block text-sm text-accent hover:text-accent/80">
                  Go to dashboard
                </Link>
              </div>
            </>
          )}
        </div>
      </motion.div>
    </div>
  );
}
