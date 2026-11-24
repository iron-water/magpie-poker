import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { verifyKey } from "../server/magpie/keys.mjs";

test("verifyKey accepts a bare host, /v1, /v1/models and trailing slashes", async () => {
  const seen = [];
  const server = createServer((req, res) => {
    seen.push({ url: req.url, auth: req.headers.authorization });
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ data: [{ id: "model-a" }, { id: "model-b" }] }));
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}`;
  try {
    for (const url of [base, `${base}/v1`, `${base}/v1/`, `${base}/v1/models`, `${base}/`]) {
      const r = await verifyKey({ url, key: "sk-test-1234567890" });
      assert.equal(r.ok, true, url);
      assert.equal(r.models, 2);
    }
    assert.deepEqual(
      seen.map((x) => x.url),
      ["/v1/models", "/v1/models", "/v1/models", "/v1/models", "/v1/models"],
    );
    assert.ok(seen.every((x) => x.auth === "Bearer sk-test-1234567890"));
  } finally {
    await new Promise((r) => server.close(r));
  }
});

test("verifyKey rejects a malformed or non-http url", async () => {
  assert.equal((await verifyKey({ url: "", key: "x" })).ok, false);
  assert.equal((await verifyKey({ url: "file:///etc/passwd", key: "x" })).ok, false);
});
