import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Sidebar } from '@/components/sidebar';
import { OverviewPage } from '@/pages/overview';
import { ProjectsPage } from '@/pages/projects';
import { EnvironmentsPage } from '@/pages/environments';
import { TeamPage } from '@/pages/team';
import { BillingPage } from '@/pages/billing';
import { SettingsPage } from '@/pages/settings';
import { LoginPage } from '@/pages/login';
import { RegisterPage } from '@/pages/register';
import { VerificationPage } from '@/pages/verification';
import { RecoveryPage } from '@/pages/recovery';
import { AccountPage } from '@/pages/account';
import { ErrorPage } from '@/pages/error';
import { ProfilePage } from '@/pages/profile';
import { useRequireAuth } from '@/hooks/use-auth';

function DashboardLayout({ children }: { children: React.ReactNode }) {
  const authed = useRequireAuth();

  // Block rendering until auth is verified — prevents content flash
  if (!authed) {
    return (
      <div className="dark bg-surface-0 min-h-screen text-white flex items-center justify-center">
        <div className="text-white/20 text-sm">Redirecting to login...</div>
      </div>
    );
  }

  return (
    <div className="dark bg-surface-0 min-h-screen text-white">
      <Sidebar />
      <main className="ml-[220px] p-8 pt-6">
        {children}
      </main>
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Auth pages — standalone layout, no sidebar */}
        <Route path="/login" element={<LoginPage />} />
        <Route path="/login/callback" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/verification" element={<VerificationPage />} />
        <Route path="/recovery" element={<RecoveryPage />} />
        <Route path="/error" element={<ErrorPage />} />

        {/* Dashboard pages — sidebar layout, auth required */}
        <Route
          path="*"
          element={
            <DashboardLayout>
              <Routes>
                <Route path="/" element={<OverviewPage />} />
                <Route path="/projects" element={<ProjectsPage />} />
                <Route path="/environments" element={<EnvironmentsPage />} />
                <Route path="/team" element={<TeamPage />} />
                <Route path="/billing" element={<BillingPage />} />
                <Route path="/settings" element={<SettingsPage />} />
                <Route path="/account" element={<AccountPage />} />
                <Route path="/profile" element={<ProfilePage />} />
              </Routes>
            </DashboardLayout>
          }
        />
      </Routes>
    </BrowserRouter>
  );
}
