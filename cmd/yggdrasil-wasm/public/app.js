// app.js

const go = new Go();

async function init() {
    const statusEl = document.getElementById("status");
    try {
        statusEl.innerText = "Loading WebAssembly...";
        const result = await WebAssembly.instantiateStreaming(fetch("yggdrasil.wasm"), go.importObject);
        go.run(result.instance);
        statusEl.innerText = "Yggdrasil Node Online!";

        // Wait a bit to ensure go has exported functions
        await new Promise(r => setTimeout(r, 100));

        if (!("serviceWorker" in navigator)) {
            statusEl.innerText = "Error: Service Workers not supported in this browser.";
            return;
        }

        const registration = await navigator.serviceWorker.register("sw.js", { scope: "./" });
        statusEl.innerText = "Service Worker Registered.";

        // We need to wait for the service worker to become active
        if (registration.installing) {
            await new Promise(resolve => {
                registration.installing.addEventListener('statechange', e => {
                    if (e.target.state === 'activated') {
                        resolve();
                    }
                });
            });
        }

        const sw = registration.active || navigator.serviceWorker.controller;
        if (!sw) {
            statusEl.innerText = "Service worker not active yet. Reload the page.";
            return;
        }

        // Setup communication channel
        const channel = new MessageChannel();
        sw.postMessage({ type: "init-ygg-port" }, [channel.port2]);

        channel.port1.onmessage = async (event) => {
            const data = event.data;
            if (data.type === "ygg-fetch") {
                const reqId = data.reqId;
                try {
                    // Call the Go Wasm function
                    const response = await YggFetch(data.url, data.method, data.body);
                    channel.port1.postMessage({
                        type: "ygg-fetch-response",
                        reqId: reqId,
                        response: response
                    });
                } catch (error) {
                    channel.port1.postMessage({
                        type: "ygg-fetch-error",
                        reqId: reqId,
                        error: error.toString()
                    });
                }
            }
        };

        statusEl.innerText = "Ready!";

        // Setup UI
        const btn = document.getElementById("go-btn");
        const input = document.getElementById("url-input");
        const frame = document.getElementById("content-frame");

        btn.onclick = () => {
            const url = input.value.trim();
            if (url) {
                frame.src = url;
            }
        };

        input.onkeypress = (e) => {
            if (e.key === 'Enter') {
                btn.click();
            }
        };

    } catch (err) {
        statusEl.innerText = "Initialization failed: " + err.message;
        console.error(err);
    }
}

init();
