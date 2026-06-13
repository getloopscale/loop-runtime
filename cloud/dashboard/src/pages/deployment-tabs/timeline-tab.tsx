import { useParams } from 'react-router-dom';
import { Card } from '@loopzero/ui';
import { useDeployment } from '@/hooks/use-deployment';
import { BuildTimelineLive } from '@/components/deploy/build-timeline-live';

/**
 * TimelineTab — Plan J-17 T5.3.
 *
 * Renders the live SSE-driven step-by-step timeline for the current
 * build. The parent layout owns BuildHeader + the tab strip; this tab
 * is content-only. `useDeployment` hits the same React Query cache the
 * layout populated, so this is zero-network.
 */
export function TimelineTab() {
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  const { build } = useDeployment(id);

  if (!build) return null;

  return (
    <Card>
      <Card.Header>
        <Card.Title>Timeline</Card.Title>
        <Card.Description>Live pipeline step-by-step progress (SSE).</Card.Description>
      </Card.Header>
      <Card.Body>
        <BuildTimelineLive buildId={build.build_id} projectName={name} />
      </Card.Body>
    </Card>
  );
}
