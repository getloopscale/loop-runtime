const S3_ENDPOINT = "https://loopzero-static-assets.sfo3.digitaloceanspaces.com";
const BASE_DOMAIN = "dev.loopzero.example";

const SECURITY_HEADERS = {
  "X-Frame-Options": "DENY",
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  "Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' https://*.dev.loopzero.example",
};

function getCacheControl(pathname) {
  if (/\.[0-9a-f]{8,}\.(js|css|png|jpg|jpeg|svg|woff2?|ttf|eot|ico|gif|webp)$/.test(pathname)) {
    return "public, max-age=31536000, immutable";
  }
  if (pathname.endsWith(".html") || pathname === "/" || !pathname.includes(".")) {
    return "no-cache, no-store, must-revalidate";
  }
  if (/\.(js|css|png|jpg|jpeg|svg|woff2?|ttf|eot|ico|gif|webp|map)$/.test(pathname)) {
    return "public, max-age=86400";
  }
  return "no-cache";
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const hostname = url.hostname;
    const suffix = "." + BASE_DOMAIN;

    // Only handle subdomains of dev.loopzero.example
    if (!hostname.endsWith(suffix)) {
      return fetch(request);
    }

    // Extract subdomain as KV key (e.g., "tn-loopzero-e2e-project-web-production")
    const subdomain = hostname.slice(0, -suffix.length);
    if (!subdomain) {
      return fetch(request);
    }

    // Lookup in KV: subdomain → JSON { buildId, s3Prefix }
    const routingData = await env.LOOPZERO_ROUTING.get(subdomain, { type: "json" });
    if (!routingData) {
      // No KV entry — this is a container app, pass through to tunnel
      return fetch(request);
    }

    const { buildId, s3Prefix } = routingData;
    let pathname = url.pathname;

    // SPA fallback: paths without file extension serve index.html
    if (!pathname.match(/\.\w+$/) || pathname === "/") {
      pathname = "/index.html";
    }

    // Build S3 URL using s3Prefix
    const s3Url = `${S3_ENDPOINT}${s3Prefix}${pathname}`;
    const s3Response = await fetch(s3Url);

    if (!s3Response.ok) {
      // If asset not found, try SPA fallback (index.html)
      if (s3Response.status === 404 && pathname !== "/index.html") {
        const fallbackUrl = `${S3_ENDPOINT}${s3Prefix}/index.html`;
        const fallbackResponse = await fetch(fallbackUrl);

        if (fallbackResponse.ok) {
          const headers = new Headers(fallbackResponse.headers);
          headers.set("Cache-Control", "no-cache, no-store, must-revalidate");
          headers.set("Content-Type", "text/html; charset=utf-8");
          for (const [key, value] of Object.entries(SECURITY_HEADERS)) {
            headers.set(key, value);
          }
          return new Response(fallbackResponse.body, { status: 200, headers });
        }
      }
      return new Response("Not Found", { status: 404, headers: SECURITY_HEADERS });
    }

    // Success — return with security headers and cache control
    const responseHeaders = new Headers(s3Response.headers);
    responseHeaders.set("Cache-Control", getCacheControl(pathname));
    for (const [key, value] of Object.entries(SECURITY_HEADERS)) {
      responseHeaders.set(key, value);
    }
    return new Response(s3Response.body, { status: s3Response.status, headers: responseHeaders });
  },
};
