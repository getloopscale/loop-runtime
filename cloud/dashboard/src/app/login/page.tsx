'use client';

import { startPKCEFlow } from '@/lib/auth';

export default function LoginPage() {
  return (
    <div className="min-h-[60vh] flex items-center justify-center">
      <div className="bg-white p-8 rounded-lg border border-gray-200 w-full max-w-md">
        <h1 className="text-2xl font-bold mb-6 text-center">Sign in to LoopZero</h1>

        <button
          onClick={() => startPKCEFlow()}
          className="w-full bg-loopzero-600 text-white py-3 px-4 rounded-lg font-medium hover:bg-loopzero-700 transition mb-4"
        >
          Continue with LoopZero Account
        </button>

        <div className="relative my-6">
          <div className="absolute inset-0 flex items-center">
            <div className="w-full border-t border-gray-200" />
          </div>
          <div className="relative flex justify-center text-sm">
            <span className="bg-white px-2 text-gray-500">or use the CLI</span>
          </div>
        </div>

        <p className="text-sm text-gray-500 text-center">
          Run <code className="bg-gray-100 px-2 py-1 rounded text-sm">loopzero login</code> in your terminal
        </p>
      </div>
    </div>
  );
}
