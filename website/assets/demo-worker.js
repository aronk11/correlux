/* Classic worker: the Go runtime and its memory stay off the page's UI thread. */
importScripts("demo/wasm_exec.js");
let ready = false;
self.onmessage = async ({ data }) => {
  try {
    if (data.boot) {
      const response = await fetch("demo/correlux.wasm.gz");
      if (!response.ok) throw new Error("Could not download the terminal.");
      const bytes = await new Response(response.body.pipeThrough(new DecompressionStream("gzip"))).arrayBuffer();
      const go = new Go();
      const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
      go.run(instance).catch((error) => self.postMessage({ error: error.message }));
      ready = true;
    }
    if (!ready) return;
    self.postMessage({ frame: JSON.parse(self.correluxDemoStep(JSON.stringify(data.input || {}))) });
  } catch (error) {
    self.postMessage({ error: error.message || "The terminal could not start." });
  }
};
