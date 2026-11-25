/* Magpie plugin hand-off page.
   The plugin frame is sandboxed and has no network/storage; the only thing it
   may do is navigate itself to the local service. The bridge is required so
   magpie considers the frame alive and can supply options/service/theme. */
(async function () {
  var DEFAULT_PORT = 3481;
  var port = DEFAULT_PORT;
  var service = "";
  var theme = "";

  function withTimeout(value, ms) {
    return Promise.race([
      Promise.resolve(value).catch(function () { return null; }),
      new Promise(function (resolve) { setTimeout(function () { resolve(null); }, ms); }),
    ]);
  }

  try {
    var ready = await withTimeout(window.magpie && window.magpie.ready, 700);
    if (ready && typeof ready.dark === "boolean") theme = ready.dark ? "dark" : "light";
  } catch (e) { /* older host / no bridge */ }

  try {
    var s = window.magpie && window.magpie.service;
    if (typeof s === "string" && /^https?:\/\//.test(s)) service = s;
  } catch (e) { /* older magpie */ }

  try {
    var o = await withTimeout(window.magpie && window.magpie.options && window.magpie.options.get && window.magpie.options.get(), 800);
    if (o && Number(o.port) > 0) port = Number(o.port);
  } catch (e) { /* default port */ }

  var q = theme ? "?theme=" + encodeURIComponent(theme) : "";
  var target = service ? service + q : "http://127.0.0.1:" + port + "/" + q;

  // A failed navigation leaves this document in place; surface a real retry
  // instead of looking stuck forever.
  setTimeout(function () {
    var said = document.getElementById("said");
    if (!said || document.getElementById("retry")) return;
    said.textContent = "本地牌桌服务没有起来（127.0.0.1:" + port + "）";
    var b = document.createElement("button");
    b.id = "retry";
    b.type = "button";
    b.textContent = "重试";
    b.addEventListener("click", function () { location.reload(); });
    said.after(b);
  }, 6000);

  location.replace(target);
})();
