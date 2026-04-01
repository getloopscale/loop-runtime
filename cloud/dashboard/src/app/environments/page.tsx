'use client';

import { useEffect, useState } from 'react';
import { apiFetch } from '@/lib/api';

interface Environment {
  name: string;
  status: string;
}

export default function EnvironmentsPage() {
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiFetch<{ environments: Environment[] }>('/v1/environments').then(data => {
      setEnvs(data?.environments || []);
      setLoading(false);
    });
  }, []);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Environments</h1>
      {loading ? (
        <p className="text-gray-500">Loading...</p>
      ) : envs.length === 0 ? (
        <div className="bg-white rounded-lg border border-gray-200 p-8 text-center">
          <p className="text-gray-500">No environments configured</p>
        </div>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 divide-y divide-gray-100">
          {envs.map(e => (
            <div key={e.name} className="px-6 py-4 flex items-center justify-between">
              <div className="font-medium">{e.name}</div>
              <span className="text-sm px-2 py-1 bg-green-50 text-green-700 rounded">{e.status}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
