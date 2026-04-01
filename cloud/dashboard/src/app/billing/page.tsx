'use client';

import { useEffect, useState } from 'react';
import { apiFetch } from '@/lib/api';

interface BillingInfo {
  plan: string;
  team: string;
  usage: Record<string, { used: number; limit: number }>;
}

export default function BillingPage() {
  const [billing, setBilling] = useState<BillingInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiFetch<BillingInfo>('/v1/billing').then(data => {
      setBilling(data);
      setLoading(false);
    });
  }, []);

  const openPortal = async () => {
    const data = await apiFetch<{ url: string }>('/v1/billing/portal');
    if (data?.url) window.open(data.url, '_blank');
  };

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Billing</h1>
      {loading ? (
        <p className="text-gray-500">Loading...</p>
      ) : billing ? (
        <div className="space-y-6">
          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <div className="flex items-center justify-between mb-4">
              <div>
                <div className="text-sm text-gray-500">Current Plan</div>
                <div className="text-2xl font-bold">{billing.plan}</div>
              </div>
              <button onClick={openPortal} className="bg-loopzero-600 text-white px-4 py-2 rounded-lg text-sm hover:bg-loopzero-700 transition">
                Manage Plan
              </button>
            </div>
          </div>

          {Object.keys(billing.usage).length > 0 && (
            <div className="bg-white rounded-lg border border-gray-200 p-6">
              <h2 className="text-lg font-semibold mb-4">Usage</h2>
              <div className="space-y-3">
                {Object.entries(billing.usage).map(([metric, { used, limit }]) => (
                  <div key={metric} className="flex items-center justify-between">
                    <span className="text-sm text-gray-600 capitalize">{metric.replace(/_/g, ' ')}</span>
                    <span className="text-sm font-mono">
                      {used} / {limit >= 999999 ? 'unlimited' : limit}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <p className="text-gray-500">Unable to load billing information.</p>
      )}
    </div>
  );
}
