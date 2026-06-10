import { ErrorBoundary } from 'react-error-boundary';
import { AlertTriangle } from 'lucide-react';
import { Button, Card } from '@loopzerokit/ui';
import type { ReactNode } from 'react';
import { reportError } from '@/lib/error-reporter';

interface RouteBoundaryProps {
  children: ReactNode;
  /** Path of the route being wrapped, surfaced in error telemetry. */
  routePath?: string;
}

interface FallbackProps {
  error: Error;
  resetErrorBoundary: () => void;
  routePath?: string;
}

function Fallback({ error, resetErrorBoundary }: FallbackProps) {
  const isDev = import.meta.env.DEV;
  return (
    <div className="min-h-[60vh] flex items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <Card.Body className="p-8 text-center space-y-4">
          <div className="flex size-12 items-center justify-center rounded-xl bg-destructive/10 border border-destructive/20 mx-auto">
            <AlertTriangle className="size-6 text-destructive" aria-hidden="true" />
          </div>
          <div className="space-y-1">
            <h1 className="font-display text-title-sm font-semibold text-foreground">
              This page crashed
            </h1>
            <p className="text-body-sm text-muted-foreground">
              {isDev
                ? error.message
                : 'Refresh the page or contact support if this keeps happening.'}
            </p>
          </div>
          <Button
            variant="primary"
            size="lg"
            className="w-full justify-center"
            onClick={resetErrorBoundary}
          >
            Retry
          </Button>
        </Card.Body>
      </Card>
    </div>
  );
}

export function RouteBoundary({ children, routePath }: RouteBoundaryProps) {
  return (
    <ErrorBoundary
      FallbackComponent={(props) => <Fallback {...props} routePath={routePath} />}
      onError={(error, info) =>
        reportError(error, { componentStack: info.componentStack, routePath })
      }
    >
      {children}
    </ErrorBoundary>
  );
}
