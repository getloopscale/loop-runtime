/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  transpilePackages: ['@dogfood/shared-utils'],
};

export default nextConfig;
