import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { DashboardLayout } from '@/components/layout';

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
      <Suspense fallback={<PageLoader />}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/verification" element={<VerificationPage />} />
          <Route path="/recovery" element={<RecoveryPage />} />
          <Route path="/device-success" element={<DeviceSuccessPage />} />
          <Route
            path="*"
            element={
              <DashboardLayout>
                <Suspense fallback={<PageLoader />}>
                  <Routes>
                    <Route path="/" element={<OverviewPage />} />
                    <Route path="/projects" element={<ProjectsPage />} />
                    <Route path="/environments" element={<EnvironmentsPage />} />
                    <Route path="/team" element={<TeamPage />} />
                    <Route path="/billing" element={<BillingPage />} />
                    <Route path="/domains" element={<DomainsPage />} />
                    <Route path="/settings" element={<SettingsPage />} />
                    <Route path="/profile" element={<ProfilePage />} />
                  </Routes>
                </Suspense>
              </DashboardLayout>
            }
          />
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}
