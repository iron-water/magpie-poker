import { startLocalServer } from "./server/local-server.mjs";

let started = null;

function boot(options = {}) {
  if (!started) {
    const port = Number(options?.port ?? process.env.MAGPIE_POKER_PORT) || 3481;
    const tablePort = Number(options?.tablePort ?? process.env.MAGPIE_POKER_TABLE_PORT) || 3482;
    started = (async () => {
      // An old plugin host can hold the page port for a few seconds while it
      // shuts down. Retry like page-chat does instead of failing the host.
      for (let attempt = 0; attempt < 30; attempt++) {
        try {
          const app = await startLocalServer({ port, tablePort });
          console.log(
            app.reused
              ? `[poker] reusing the service already on http://127.0.0.1:${app.port}`
              : `[poker] ui on http://127.0.0.1:${app.port}`,
          );
          return app;
        } catch (err) {
          if (err?.code !== "EADDRINUSE") {
            console.error(`[poker] could not start: ${err?.message || err}`);
            return null;
          }
          if (attempt === 0) console.log(`[poker] port ${port} is still held; retrying`);
          await new Promise((r) => setTimeout(r, 1000));
        }
      }
      console.error(`[poker] could not start: port ${port} stayed busy`);
      return null;
    })();
  }
  return started;
}

// magpie's Bun host calls every exported function as a plugin entry. Only the
// server hook is needed: it starts the page service in the host process.
export default {
  id: "magpie-poker",
  server: async (_input, options) => {
    await boot(options);
    return {};
  },
};
