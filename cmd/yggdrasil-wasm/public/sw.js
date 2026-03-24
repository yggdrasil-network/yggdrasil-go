// sw.js

let yggPort = null;
let requestCounter = 0;
const pendingRequests = new Map();

self.addEventListener("install", (event) => {
    self.skipWaiting();
});

self.addEventListener("activate", (event) => {
    event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
    if (event.data && event.data.type === "init-ygg-port") {
        yggPort = event.ports[0];
        yggPort.onmessage = (msgEvent) => {
            const data = msgEvent.data;
            if (data.type === "ygg-fetch-response") {
                const reqId = data.reqId;
                if (pendingRequests.has(reqId)) {
                    const resolve = pendingRequests.get(reqId);
                    pendingRequests.delete(reqId);
                    resolve(data.response);
                }
            } else if (data.type === "ygg-fetch-error") {
                const reqId = data.reqId;
                if (pendingRequests.has(reqId)) {
                    const resolve = pendingRequests.get(reqId);
                    pendingRequests.delete(reqId);
                    resolve(new Response("Gateway Error: " + data.error, { status: 502, statusText: "Bad Gateway" }));
                }
            }
        };
    }
});

self.addEventListener("fetch", (event) => {
    const url = new URL(event.request.url);

    // Filter requests for .ygg or 0200::/7 IPv6 block
    const isYggdrasilRequest = url.hostname.endsWith(".ygg") ||
        url.hostname.startsWith("[02") || url.hostname.startsWith("[03") ||
        url.hostname.startsWith("02") || url.hostname.startsWith("03") ||
        url.hostname.startsWith("[2") || url.hostname.startsWith("[3") ||
        url.hostname.startsWith("2") || url.hostname.startsWith("3");

    if (isYggdrasilRequest) {
        event.respondWith(
            new Promise((resolve) => {
                if (!yggPort) {
                    resolve(new Response("Yggdrasil WebAssembly node is not ready yet.", { status: 503 }));
                    return;
                }

                const reqId = requestCounter++;
                pendingRequests.set(reqId, resolve);

                // Prepare request data
                event.request.text().then((bodyText) => {
                    yggPort.postMessage({
                        type: "ygg-fetch",
                        reqId: reqId,
                        url: event.request.url,
                        method: event.request.method,
                        body: bodyText,
                    });
                }).catch(() => {
                    // In case there is no body or it can't be read as text
                    yggPort.postMessage({
                        type: "ygg-fetch",
                        reqId: reqId,
                        url: event.request.url,
                        method: event.request.method,
                        body: "",
                    });
                });
            }).then((respData) => {
                if (respData instanceof Response) return respData;

                const headers = new Headers();
                if (respData.headers) {
                    for (const [key, value] of Object.entries(respData.headers)) {
                        // Strip security headers that prevent iframe rendering
                        const lowerKey = key.toLowerCase();
                        if (lowerKey === "x-frame-options" || lowerKey === "content-security-policy") {
                            continue;
                        }
                        headers.set(key, value);
                    }
                }

                // Default content type if not set
                if (!headers.has("Content-Type")) {
                    headers.set("Content-Type", "text/html");
                }

                return new Response(respData.bodyBytes, {
                    status: respData.status || 200,
                    statusText: respData.statusText || "OK",
                    headers: headers
                });
            })
        );
    }
});
