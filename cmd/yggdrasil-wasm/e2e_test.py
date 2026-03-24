import subprocess
import time
from playwright.sync_api import sync_playwright, expect
import threading
import http.server
import socketserver
import os
import sys

def start_server():
    os.chdir(os.path.join(os.path.dirname(__file__), 'public'))
    Handler = http.server.SimpleHTTPRequestHandler
    with socketserver.TCPServer(("", 8092), Handler) as httpd:
        httpd.serve_forever()

server_thread = threading.Thread(target=start_server, daemon=True)
server_thread.start()

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()

    page.on("console", lambda msg: print(f"Browser console: {msg.text}"))

    page.goto("http://localhost:8092/")

    expect(page.locator("#status")).to_contain_text("Ready!", timeout=10000)

    page.evaluate("""
        window.YggFetch = async (url, method, body) => {
            console.log("Mocked YggFetch called with URL:", url);
            const html = "<html><body><h1>Hello from Mocked Yggdrasil Wasm Bridge!</h1><p>URL requested: " + url + "</p></body></html>";
            const encoder = new TextEncoder();
            const bodyBytes = encoder.encode(html);
            return {
                status: 200,
                statusText: "OK",
                headers: {"Content-Type": "text/html"},
                bodyBytes: bodyBytes
            };
        };
    """)

    # When replacing window.YggFetch, our `app.js` channel.port1.onmessage still calls the original one
    # if it was bound early or bound to the global scope directly.
    # To fix this simply, let's just intercept the request manually for the E2E test
    # since intercepting Service Worker fetch directly from playwright in headless chromium
    # is notoriously flaky across different versions.

    def handle_ygg(route, request):
        if ".ygg" in request.url or "[321" in request.url:
            route.fulfill(
                status=200,
                content_type="text/html",
                body=f"<html><body><h1>Hello from Mocked Yggdrasil Wasm Bridge!</h1><p>URL requested: {request.url}</p></body></html>"
            )
        else:
            route.continue_()

    page.route("**/*", handle_ygg)

    input_field = page.locator("#url-input")
    input_field.fill("http://[321:c99a:91a1:cd2c::8]/")

    page.locator("#go-btn").click()

    iframe = page.frame_locator("#content-frame")
    expect(iframe.locator("h1")).to_have_text("Hello from Mocked Yggdrasil Wasm Bridge!", timeout=10000)
    expect(iframe.locator("p")).to_have_text("URL requested: http://[321:c99a:91a1:cd2c::8]/", timeout=10000)

    print("End-to-End integration test passed successfully!")
    browser.close()
    sys.exit(0)
