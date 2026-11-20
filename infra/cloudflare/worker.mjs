const S3_ENDPOINT = "https://loopzero-static-assets.sfo3.digitaloceanspaces.com";
const BASE_DOMAIN = "dev.getloopscale.com";

const SECURITY_HEADERS = {
  "X-Frame-Options": "DENY",
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  // Issue #232/#235 — `script-src` no longer carries 'unsafe-inline'.
  //
  // This worker fronts tenant static content on *.dev.getloopscale.com, so this CSP is
  // the last header those users receive. With 'unsafe-inline' any injected inline
  // <script> in the served HTML executed, which negated most of what the policy
  // was there to buy; the blast radius was every tenant's static surface.
  //
  // A nonce is not an option here: the worker proxies bytes from object storage
  // and never parses the HTML, so it has nothing to stamp. The fix is therefore
  // to require external scripts — which is what the bundlers in use already emit
  // (hashed filenames, no inline bootstrap).
  //
  // 'unsafe-inline' is KEPT on style-src, deliberately. Inline styles do not
  // execute code, and bundling the two would have let the urgent problem be
  // deferred alongside the cosmetic one. Hardening it is its own change.
  //
  // Guarded by `loopzero-ops quality worker-csp`.
  "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' https://*.dev.getloopscale.com",
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

// routeToCell sends a dynamic app's request to the ingress of the cell that serves it
// (#448). The engine publishes route:<label> for a platform host and route:<fqdn> for a
// custom domain, each → { cellId }; the edge's own network maps a cell to its ingress,
// ingress:<cellId> → { resolveOverride }. The request keeps its URL and Host — the cell
// routes by Host — and is resolved to the ingress hostname: a Worker cannot fetch a raw
// IP. Returns null when no route is published; a published route whose cell has no
// ingress is refused with 503 rather than sent anywhere else.
async function routeToCell(request, routeKey) {
  var route = await LOOPZERO_ROUTING.get("route:" + routeKey, { type: "json" });
  if (!route || !route.cellId) {
    return null;
  }
  var ingress = await LOOPZERO_ROUTING.get("ingress:" + route.cellId, { type: "json" });
  if (!ingress || !ingress.resolveOverride) {
    return new Response("Service temporarily unavailable", { status: 503, headers: SECURITY_HEADERS });
  }
  return fetch(request, { cf: { resolveOverride: ingress.resolveOverride } });
}

async function handleRequest(request) {
  var url = new URL(request.url);
  var hostname = url.hostname;
  var suffix = "." + BASE_DOMAIN;

  if (!hostname.endsWith(suffix)) {
    // A custom domain: routed by its full name when the engine published it.
    var custom = await routeToCell(request, hostname);
    return custom || fetch(request);
  }

  var subdomain = hostname.slice(0, -suffix.length);
  if (!subdomain) {
    return fetch(request);
  }

  // 1. Static app routing (S3/CDN delivery).
  var routingData = await LOOPZERO_ROUTING.get(subdomain, { type: "json" });
  if (!routingData) {
    // 2. Dynamic app routing by cell (#448).
    var routed = await routeToCell(request, subdomain);
    // 3. Fallback: pass through to the default origin.
    return routed || fetch(request);
  }

  // Static app routing (existing logic — unchanged)
  var buildId = routingData.buildId;
  var s3Prefix = routingData.s3Prefix;
  var pathname = url.pathname;

  if (!pathname.match(/\.\w+$/) || pathname === "/") {
    pathname = "/index.html";
  }

  var s3Url = S3_ENDPOINT + s3Prefix + pathname;
  var s3Response = await fetch(s3Url);

  if (!s3Response.ok) {
    if (s3Response.status === 404 && pathname !== "/index.html") {
      var fallbackUrl = S3_ENDPOINT + s3Prefix + "/index.html";
      var fallbackResponse = await fetch(fallbackUrl);

      if (fallbackResponse.ok) {
        var headers = new Headers(fallbackResponse.headers);
        headers.set("Cache-Control", "no-cache, no-store, must-revalidate");
        headers.set("Content-Type", "text/html; charset=utf-8");
        var keys = Object.keys(SECURITY_HEADERS);
        for (var i = 0; i < keys.length; i++) {
          headers.set(keys[i], SECURITY_HEADERS[keys[i]]);
        }
        return new Response(fallbackResponse.body, { status: 200, headers: headers });
      }
    }
    return new Response("Not Found", { status: 404, headers: SECURITY_HEADERS });
  }

  var responseHeaders = new Headers(s3Response.headers);
  responseHeaders.set("Cache-Control", getCacheControl(pathname));
  var secKeys = Object.keys(SECURITY_HEADERS);
  for (var j = 0; j < secKeys.length; j++) {
    responseHeaders.set(secKeys[j], SECURITY_HEADERS[secKeys[j]]);
  }
  return new Response(s3Response.body, { status: s3Response.status, headers: responseHeaders });
}

addEventListener("fetch", function(event) {
  event.respondWith(handleRequest(event.request));
});
