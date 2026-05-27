import { ErrorBoundary } from 'react-error-boundary';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@loopzero/ui';
import type { ReactNode } from 'react';
import { reportError } from '@/lib/error-reporter';

interface SheetBoundaryProps {
  children: ReactNode;
  /** Invoked when user clicks "Close" — parent should close the Sheet. */
  onClose: () => void;
  /** Sheet identifier, surfaced as routePath="sheet:{name}" in telemetry. */
  sheetName?: string;
}

interface FallbackProps {
  error: Error;
  onClose: () => void;
}

function Fallback({ error, onClose }: FallbackProps) {
  const isDev = import.meta.env.DEV;
  return (
    <div className="p-6 space-y-4">
      <div className="flex items-start gap-3">
        <AlertTriangle
          className="size-5 text-destructive shrink-0 mt-0.5"
          aria-hidden="true"
        />
        <div className="space-y-1">
          <p className="font-medium text-body-sm text-foreground">
            This form crashed.
          </p>
          <p className="text-body-sm text-muted-foreground">
            {isDev ? error.message : 'Close this form and try again.'}
          </p>
        </div>
      </div>
      <Button variant="secondary" size="md" className="w-full" onClick={onClose}>
        Close
      </Button>
    </div>
  );
}

export function SheetBoundary({ children, onClose, sheetName }: SheetBoundaryProps) {
  return (
    <ErrorBoundary
      FallbackComponent={(props) => <Fallback error={props.error} onClose={onClose} />}
      onError={(error, info) =>
        reportError(error, {
          componentStack: info.componentStack,
          routePath: sheetName ? `sheet:${sheetName}` : 'sheet:unknown',
        })
      }
    >
      {children}
    </ErrorBoundary>
  );
}
