import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'LoopZero — Dashboard',
  description: 'Deploy platform for monorepo backends',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-gray-50 text-gray-900 min-h-screen">
        <nav className="bg-white border-b border-gray-200 px-6 py-3 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <a href="/" className="text-xl font-bold text-loopzero-700">LoopZero</a>
            <a href="/projects" className="text-sm text-gray-600 hover:text-gray-900">Projects</a>
            <a href="/environments" className="text-sm text-gray-600 hover:text-gray-900">Environments</a>
            <a href="/team" className="text-sm text-gray-600 hover:text-gray-900">Team</a>
            <a href="/billing" className="text-sm text-gray-600 hover:text-gray-900">Billing</a>
            <a href="/settings" className="text-sm text-gray-600 hover:text-gray-900">Settings</a>
          </div>
        </nav>
        <main className="max-w-6xl mx-auto px-6 py-8">
          {children}
        </main>
      </body>
    </html>
  );
}
