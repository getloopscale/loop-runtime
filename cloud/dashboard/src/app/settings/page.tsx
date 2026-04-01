'use client';

export default function SettingsPage() {
  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Settings</h1>
      <div className="bg-white rounded-lg border border-gray-200 p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Account</h2>
        <p className="text-sm text-gray-500">
          Account settings are managed via the CLI. Run <code className="bg-gray-100 px-2 py-1 rounded">loopzero whoami</code> to view your account details.
        </p>
      </div>

      <div className="bg-white rounded-lg border border-red-200 p-6">
        <h2 className="text-lg font-semibold text-red-600 mb-4">Danger Zone</h2>
        <p className="text-sm text-gray-500 mb-4">
          To delete your account and all projects, run: <code className="bg-gray-100 px-2 py-1 rounded">loopzero project delete --all</code>
        </p>
      </div>
    </div>
  );
}
