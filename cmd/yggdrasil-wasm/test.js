const fs = require('fs');

global.crypto = require('crypto');
require('./public/wasm_exec.js');

const go = new Go();
const wasmBuffer = fs.readFileSync('./public/yggdrasil.wasm');

WebAssembly.instantiate(wasmBuffer, go.importObject).then((result) => {
    go.run(result.instance);

    // Check if the YggFetch function has been registered on the global object
    if (typeof global.YggFetch !== 'function') {
        console.error("YggFetch was not registered on the global object");
        process.exit(1);
    }

    console.log("WASM module loaded and initialized successfully.");
    process.exit(0);
}).catch((err) => {
    console.error("Failed to load or execute WASM module:", err);
    process.exit(1);
});