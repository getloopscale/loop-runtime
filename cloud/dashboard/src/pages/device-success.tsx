import { CheckCircle2 } from 'lucide-react';
import { Card } from '@loopzero/ui';

export function DeviceSuccessPage() {
  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 text-center space-y-4">
          <CheckCircle2
            className="size-12 text-success mx-auto"
            aria-hidden="true"
          />
          <div className="space-y-1">
            <h1 className="font-display text-title-sm font-semibold text-foreground">
              CLI authorized
            </h1>
            <p className="text-body-sm text-muted-foreground">
              You can close this tab and return to your terminal.
            </p>
          </div>
        </Card.Body>
      </Card>
    </div>
  );
}
