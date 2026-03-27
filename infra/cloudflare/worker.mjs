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

async function handleRequest(request) {
  var url = new URL(request.url);
  var hostname = url.hostname;
  var suffix = "." + BASE_DOMAIN;

  if (!hostname.endsWith(suffix)) {
    return fetch(request);
  }

  var subdomain = hostname.slice(0, -suffix.length);
  if (!subdomain) {
    return fetch(request);
  }

  var routingData = await LOOPZERO_ROUTING.get(subdomain, { type: "json" });
  if (!routingData) {
    return fetch(request);
  }

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
