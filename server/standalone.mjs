import { startLocalServer } from "./local-server.mjs";

const port = Number(process.env.POKER_PORT || process.env.MAGPIE_POKER_PORT) || 3481;
const tablePort = Number(process.env.POKER_TABLE_PORT || process.env.MAGPIE_POKER_TABLE_PORT) || 3482;
const app = await startLocalServer({ port, tablePort });
console.log(`magpie-poker: http://127.0.0.1:${app.port}/`);
console.log("Ctrl+C to stop.");
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, async () => {
    await app.close();
    process.exit(0);
  });
}
