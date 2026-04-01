'use client';

import { useEffect, useState } from 'react';
import { apiFetch } from '@/lib/api';

interface Project {
  name: string;
  created_at: string;
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiFetch<{ projects: Project[] }>('/v1/projects').then(data => {
      setProjects(data?.projects || []);
      setLoading(false);
    });
  }, []);

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Projects</h1>
      </div>

      {loading ? (
        <p className="text-gray-500">Loading...</p>
      ) : projects.length === 0 ? (
        <div className="bg-white rounded-lg border border-gray-200 p-8 text-center">
          <p className="text-gray-500 mb-4">No projects yet</p>
          <p className="text-sm text-gray-400">
            Create your first project: <code className="bg-gray-100 px-2 py-1 rounded">loopzero init</code>
          </p>
        </div>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 divide-y divide-gray-100">
          {projects.map(p => (
            <a key={p.name} href={`/projects/${p.name}`} className="block px-6 py-4 hover:bg-gray-50 transition">
              <div className="font-medium">{p.name}</div>
              <div className="text-sm text-gray-500">{new Date(p.created_at).toLocaleDateString()}</div>
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
