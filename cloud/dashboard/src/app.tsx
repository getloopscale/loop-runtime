import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Sidebar } from '@/components/sidebar';
import { VerificationBanner } from '@/components/verification-banner';
import { OverviewPage } from '@/pages/overview';
import { ProjectsPage } from '@/pages/projects';
import { EnvironmentsPage } from '@/pages/environments';
import { TeamPage } from '@/pages/team';
import { BillingPage } from '@/pages/billing';
import { SettingsPage } from '@/pages/settings';
import { ProfilePage } from '@/pages/profile';
import { DomainsPage } from '@/pages/domains';
import { LoginPage } from '@/pages/login';
import { RegisterPage } from '@/pages/register';
import { VerificationPage } from '@/pages/verification';
import { DeviceSuccessPage } from '@/pages/device-success';
import { useRequireAuth } from '@/hooks/use-auth';
import { isEmailVerified } from '@/lib/auth';
import { Loader2 } from 'lucide-react';

function DashboardLayout({ children }: { children: React.ReactNode }) {
  const session = useRequireAuth();

  if (session === null) {
    return (
      <div className="bg-surface-0 min-h-screen text-white flex items-center justify-center">
        <Loader2 className="h-6 w-6 text-accent animate-spin" />
      </div>
    );
  }

  const verified = isEmailVerified(session);

  return (
    <div className="bg-surface-0 min-h-screen text-white">
      <Sidebar />
      <main className="ml-[220px] p-8 pt-6">
        {!verified && <VerificationBanner />}
        {children}
      </main>
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/verification" element={<VerificationPage />} />
        <Route path="/device-success" element={<DeviceSuccessPage />} />
        <Route path="*" element={
          <DashboardLayout>
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
          </DashboardLayout>
        } />
      </Routes>
    </BrowserRouter>
  );
}
