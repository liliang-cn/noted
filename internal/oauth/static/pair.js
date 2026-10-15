// Polls the pairing until the App approves it (or it is denied or expires),
// then sends the browser back to the client.
(() => {
  "use strict";
  const root = document.getElementById("pair");
  const poll = root && root.dataset.poll;
  if (!poll) return;
  const msg = document.getElementById("pair-msg");
  const live = document.getElementById("pair-live");
  const leftEl = document.getElementById("pair-left");
  const deadline = Date.now() + Number(root.dataset.left || 0) * 1000;
  let timer = 0;

  function show(text) {
    if (!text) return;
    msg.textContent = text;
    msg.hidden = false;
  }

  function end(text) {
    clearTimeout(timer);
    if (live) live.hidden = true;
    show(text);
  }

  function tick() {
    if (!leftEl) return;
    const s = Math.max(0, Math.round((deadline - Date.now()) / 1000));
    leftEl.textContent = Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
    if (s > 0) setTimeout(tick, 1000);
  }

  async function check() {
    let st;
    try {
      const r = await fetch("/oauth/pair/status?p=" + encodeURIComponent(poll), { cache: "no-store" });
      st = await r.json();
    } catch (e) {
      timer = setTimeout(check, 4000);
      return;
    }
    switch (st.status) {
      case "approved":
      case "denied":
        end(st.message);
        if (st.redirect) location.replace(st.redirect);
        return;
      case "expired":
        // Let the message be read, then tell the client so it stops waiting.
        end(st.message);
        if (st.redirect) setTimeout(() => location.replace(st.redirect), 3000);
        return;
      case "done":
      case "gone":
        end(st.message);
        return;
    }
    timer = setTimeout(check, 2000);
  }

  tick();
  check();
})();
