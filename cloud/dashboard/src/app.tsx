import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { DashboardLayout } from '@/components/layout';
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
import { RecoveryPage } from '@/pages/recovery';
import { DeviceSuccessPage } from '@/pages/device-success';

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/verification" element={<VerificationPage />} />
        <Route path="/recovery" element={<RecoveryPage />} />
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
