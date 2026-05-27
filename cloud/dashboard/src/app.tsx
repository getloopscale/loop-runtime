import { lazy, Suspense, type ReactNode } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { NuqsAdapter } from 'nuqs/adapters/react-router/v7';
import {
  Bell,
  Key,
  Link2,
  Loader2,
  Lock,
  Receipt,
  ShieldAlert,
  Users,
  Webhook,
} from 'lucide-react';
import { DashboardLayout } from '@/components/layout';
import { SubPagePlaceholder } from '@/pages/sub-page-placeholder';
import { RouteBoundary } from '@/components/error/route-boundary';

/**
 * T0.1: helper para wrap cada <Route element> em <RouteBoundary>.
 * Crash em uma page só renderiza fallback nessa page — sidebar/topbar persistem.
 * routePath é forwarded para telemetry (error reporter classifica por path).
 */
function Boundaried({ path, children }: { path: string; children: ReactNode }) {
  return <RouteBoundary routePath={path}>{children}</RouteBoundary>;
}

/**
 * Route-level code-split via React.lazy + Suspense.
 *
 * Each page becomes its own chunk; initial bundle ships only the
 * BrowserRouter + DashboardLayout shell + loading fallback. Pages load
 * on-demand when route matches.
 *
 * Pages use named exports — wrapper converts to React.lazy default-export
 * contract: `.then(m => ({ default: m.PageName }))`.
 *
 * Reduces initial bundle ≥30% for first paint (per T-H.3 attempt to meet
 * D7 +30% gzip target).
 */
const OverviewPage = lazy(() =>
  import('@/pages/overview').then((m) => ({ default: m.OverviewPage })),
);
const ProjectsPage = lazy(() =>
  import('@/pages/projects').then((m) => ({ default: m.ProjectsPage })),
);
const EnvironmentsPage = lazy(() =>
  import('@/pages/environments').then((m) => ({ default: m.EnvironmentsPage })),
);
const TeamPage = lazy(() =>
  import('@/pages/team').then((m) => ({ default: m.TeamPage })),
);
const BillingPage = lazy(() =>
  import('@/pages/billing').then((m) => ({ default: m.BillingPage })),
);
const SettingsPage = lazy(() =>
  import('@/pages/settings').then((m) => ({ default: m.SettingsPage })),
);
const ProfilePage = lazy(() =>
  import('@/pages/profile').then((m) => ({ default: m.ProfilePage })),
);
const DomainsPage = lazy(() =>
  import('@/pages/domains').then((m) => ({ default: m.DomainsPage })),
);
const LoginPage = lazy(() =>
  import('@/pages/login').then((m) => ({ default: m.LoginPage })),
);
const RegisterPage = lazy(() =>
  import('@/pages/register').then((m) => ({ default: m.RegisterPage })),
);
const VerificationPage = lazy(() =>
  import('@/pages/verification').then((m) => ({ default: m.VerificationPage })),
);
const RecoveryPage = lazy(() =>
  import('@/pages/recovery').then((m) => ({ default: m.RecoveryPage })),
);
const DeviceSuccessPage = lazy(() =>
  import('@/pages/device-success').then((m) => ({ default: m.DeviceSuccessPage })),
);
const ProjectDetailPage = lazy(() =>
  import('@/pages/project-detail').then((m) => ({ default: m.ProjectDetailPage })),
);
const NotFoundPage = lazy(() =>
  import('@/pages/not-found').then((m) => ({ default: m.NotFoundPage })),
);
const DeploymentsPage = lazy(() =>
  import('@/pages/deployments').then((m) => ({ default: m.DeploymentsPage })),
);
const DeploymentDetailPage = lazy(() =>
  import('@/pages/deployment-detail').then((m) => ({ default: m.DeploymentDetailPage })),
);
const BuildLogsPage = lazy(() =>
  import('@/pages/build-logs').then((m) => ({ default: m.BuildLogsPage })),
);
const BuildSBOMPage = lazy(() =>
  import('@/pages/build-sbom').then((m) => ({ default: m.BuildSBOMPage })),
);
const BuildAttestationPage = lazy(() =>
  import('@/pages/build-attestation').then((m) => ({ default: m.BuildAttestationPage })),
);
const RuntimeLogsPage = lazy(() =>
  import('@/pages/runtime-logs').then((m) => ({ default: m.RuntimeLogsPage })),
);
const ProjectStatusPage = lazy(() =>
  import('@/pages/project-status').then((m) => ({ default: m.ProjectStatusPage })),
);
const SecretsPage = lazy(() =>
  import('@/pages/secrets').then((m) => ({ default: m.SecretsPage })),
);
const PreviewsPage = lazy(() =>
  import('@/pages/previews').then((m) => ({ default: m.PreviewsPage })),
);
const ProjectConnectPage = lazy(() =>
  import('@/pages/project-connect').then((m) => ({ default: m.ProjectConnectPage })),
);
const AuditPage = lazy(() =>
  import('@/pages/audit').then((m) => ({ default: m.AuditPage })),
);
const ProjectScalePage = lazy(() =>
  import('@/pages/project-scale').then((m) => ({ default: m.ProjectScalePage })),
);

function PageLoader() {
  return (
    <div className="min-h-screen bg-background flex items-center justify-center">
      <Loader2
        className="size-6 text-primary animate-spin"
        aria-label="Loading page"
      />
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <NuqsAdapter>
        <Suspense fallback={<PageLoader />}>
          <Routes>
          <Route path="/login" element={<Boundaried path="/login"><LoginPage /></Boundaried>} />
          <Route path="/register" element={<Boundaried path="/register"><RegisterPage /></Boundaried>} />
          <Route path="/verification" element={<Boundaried path="/verification"><VerificationPage /></Boundaried>} />
          <Route path="/recovery" element={<Boundaried path="/recovery"><RecoveryPage /></Boundaried>} />
          <Route path="/device-success" element={<Boundaried path="/device-success"><DeviceSuccessPage /></Boundaried>} />
          <Route
            path="*"
            element={
              <DashboardLayout>
                <Suspense fallback={<PageLoader />}>
                  <Routes>
                    <Route path="/" element={<Boundaried path="/"><OverviewPage /></Boundaried>} />
                    <Route path="/projects" element={<Boundaried path="/projects"><ProjectsPage /></Boundaried>} />
                    <Route path="/projects/:name" element={<Boundaried path="/projects/:name"><ProjectDetailPage /></Boundaried>} />
                    <Route
                      path="/projects/:name/deployments"
                      element={<Boundaried path="/projects/:name/deployments"><DeploymentsPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/deployments/:id"
                      element={<Boundaried path="/projects/:name/deployments/:id"><DeploymentDetailPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/deployments/:id/logs"
                      element={<Boundaried path="/projects/:name/deployments/:id/logs"><BuildLogsPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/deployments/:id/sbom"
                      element={<Boundaried path="/projects/:name/deployments/:id/sbom"><BuildSBOMPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/deployments/:id/attestation"
                      element={<Boundaried path="/projects/:name/deployments/:id/attestation"><BuildAttestationPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/logs"
                      element={<Boundaried path="/projects/:name/logs"><RuntimeLogsPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/status"
                      element={<Boundaried path="/projects/:name/status"><ProjectStatusPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/secrets"
                      element={<Boundaried path="/projects/:name/secrets"><SecretsPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/previews"
                      element={<Boundaried path="/projects/:name/previews"><PreviewsPage /></Boundaried>}
                    />
                    <Route
                      path="/projects/:name/connect"
                      element={<Boundaried path="/projects/:name/connect"><ProjectConnectPage /></Boundaried>}
                    />
                    <Route path="/audit" element={<Boundaried path="/audit"><AuditPage /></Boundaried>} />
                    <Route
                      path="/projects/:name/scale"
                      element={<Boundaried path="/projects/:name/scale"><ProjectScalePage /></Boundaried>}
                    />
                    <Route path="/environments" element={<Boundaried path="/environments"><EnvironmentsPage /></Boundaried>} />
                    <Route path="/team" element={<Boundaried path="/team"><TeamPage /></Boundaried>} />
                    <Route path="/billing" element={<Boundaried path="/billing"><BillingPage /></Boundaried>} />
                    <Route path="/domains" element={<Boundaried path="/domains"><DomainsPage /></Boundaried>} />
                    <Route path="/settings" element={<Boundaried path="/settings"><SettingsPage /></Boundaried>} />
                    <Route
                      path="/settings/members"
                      element={
                        <Boundaried path="/settings/members">
                          <SubPagePlaceholder
                            title="Members"
                            description="Manage workspace members, roles, and pending invites."
                            icon={Users}
                            cliCommand="loopzero team list"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/settings/billing"
                      element={
                        <Boundaried path="/settings/billing">
                          <SubPagePlaceholder
                            title="Billing settings"
                            description="Payment method, invoices, and plan management."
                            icon={Receipt}
                            cliCommand="loopzero billing status"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/settings/tokens"
                      element={
                        <Boundaried path="/settings/tokens">
                          <SubPagePlaceholder
                            title="API Tokens"
                            description="Personal access tokens for the API + CLI."
                            icon={Key}
                            cliCommand="loopzero token create --name my-token"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/settings/webhooks"
                      element={
                        <Boundaried path="/settings/webhooks">
                          <SubPagePlaceholder
                            title="Webhooks"
                            description="HTTP callbacks for deploy + build events."
                            icon={Webhook}
                            cliCommand="loopzero webhook list"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/settings/notifications"
                      element={
                        <Boundaried path="/settings/notifications">
                          <SubPagePlaceholder
                            title="Notifications"
                            description="Email + integrations for build, deploy, and alert events."
                            icon={Bell}
                            cliCommand="loopzero notify list"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/settings/danger"
                      element={
                        <Boundaried path="/settings/danger">
                          <SubPagePlaceholder
                            title="Danger Zone"
                            description="Destructive workspace actions: delete, transfer ownership."
                            icon={ShieldAlert}
                            cliCommand="loopzero team delete --confirm"
                          />
                        </Boundaried>
                      }
                    />
                    <Route path="/profile" element={<Boundaried path="/profile"><ProfilePage /></Boundaried>} />
                    <Route
                      path="/profile/security"
                      element={
                        <Boundaried path="/profile/security">
                          <SubPagePlaceholder
                            title="Security"
                            description="Password, MFA, and recovery options."
                            icon={Lock}
                            cliCommand="loopzero login --rotate-credentials"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/profile/sessions"
                      element={
                        <Boundaried path="/profile/sessions">
                          <SubPagePlaceholder
                            title="Active sessions"
                            description="Devices and browsers currently signed in to your account."
                            icon={Link2}
                            cliCommand="loopzero session list"
                          />
                        </Boundaried>
                      }
                    />
                    <Route
                      path="/profile/danger"
                      element={
                        <Boundaried path="/profile/danger">
                          <SubPagePlaceholder
                            title="Danger Zone"
                            description="Destructive account actions: delete account."
                            icon={ShieldAlert}
                            cliCommand="loopzero account delete --confirm"
                          />
                        </Boundaried>
                      }
                    />
                    <Route path="*" element={<Boundaried path="*"><NotFoundPage /></Boundaried>} />
                  </Routes>
                </Suspense>
              </DashboardLayout>
            }
          />
          </Routes>
        </Suspense>
      </NuqsAdapter>
    </BrowserRouter>
  );
}
