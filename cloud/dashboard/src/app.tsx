import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Sidebar } from '@/components/sidebar';
import { OverviewPage } from '@/pages/overview';
import { ProjectsPage } from '@/pages/projects';
import { EnvironmentsPage } from '@/pages/environments';
import { TeamPage } from '@/pages/team';
import { BillingPage } from '@/pages/billing';
import { SettingsPage } from '@/pages/settings';
import { ProfilePage } from '@/pages/profile';
import { LoginPage } from '@/pages/login';
import { useRequireAuth } from '@/hooks/use-auth';

function DashboardLayout({ children }: { children: React.ReactNode }) {
  const authed = useRequireAuth();
  if (!authed) return null; // Redirect happening — render nothing

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
        {/* Public routes */}
        <Route path="/login" element={<LoginPage />} />
        <Route path="/login/callback" element={<LoginPage />} />

        {/* Protected routes */}
        <Route path="*" element={
          <DashboardLayout>
            <Routes>
              <Route path="/" element={<OverviewPage />} />
              <Route path="/projects" element={<ProjectsPage />} />
              <Route path="/environments" element={<EnvironmentsPage />} />
              <Route path="/team" element={<TeamPage />} />
              <Route path="/billing" element={<BillingPage />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="/profile" element={<ProfilePage />} />
            </Routes>
          </DashboardLayout>
        } />
      </Routes>
    </BrowserRouter>
  );
}
