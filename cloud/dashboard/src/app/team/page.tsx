'use client';

import { useEffect, useState } from 'react';
import { apiFetch } from '@/lib/api';

interface Member {
  username: string;
  email: string;
  role: string;
}

export default function TeamPage() {
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiFetch<{ members: Member[] }>('/v1/teams/members').then(data => {
      setMembers(data?.members || []);
      setLoading(false);
    });
  }, []);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Team</h1>
      {loading ? (
        <p className="text-gray-500">Loading...</p>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 divide-y divide-gray-100">
          {members.map(m => (
            <div key={m.username} className="px-6 py-4 flex items-center justify-between">
              <div>
                <div className="font-medium">{m.username}</div>
                <div className="text-sm text-gray-500">{m.email}</div>
              </div>
              <span className="text-sm px-2 py-1 bg-gray-100 text-gray-700 rounded capitalize">{m.role}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
