export default function HomePage() {
  return (
    <div>
      <h1 className="text-2xl font-bold mb-4">Dashboard</h1>
      <p className="text-gray-600 mb-6">Welcome to LoopZero. Deploy your monorepo in seconds.</p>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <a href="/projects" className="block p-6 bg-white rounded-lg border border-gray-200 hover:border-loopzero-500 transition">
          <h2 className="text-lg font-semibold mb-2">Projects</h2>
          <p className="text-sm text-gray-500">View and manage your projects</p>
        </a>
        <a href="/team" className="block p-6 bg-white rounded-lg border border-gray-200 hover:border-loopzero-500 transition">
          <h2 className="text-lg font-semibold mb-2">Team</h2>
          <p className="text-sm text-gray-500">Manage members and roles</p>
        </a>
        <a href="/billing" className="block p-6 bg-white rounded-lg border border-gray-200 hover:border-loopzero-500 transition">
          <h2 className="text-lg font-semibold mb-2">Billing</h2>
          <p className="text-sm text-gray-500">View your plan and usage</p>
        </a>
      </div>
    </div>
  );
}
