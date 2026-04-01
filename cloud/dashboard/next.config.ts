import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  output: 'export', // Static export for CF Worker + DO Spaces delivery
  images: { unoptimized: true }, // No Image Optimization in static export
};

export default nextConfig;
