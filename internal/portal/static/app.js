"use strict";

(function () {
  const $ = (id) => document.getElementById(id);
  let me = null;          // the signed-in account, or null
  let mode = "login";     // auth dialog: "login" or "signup"
  let mcpURL = location.origin + "/mcp";
  let shownTokenID = ""; // the token whose setup snippets are on screen

  // ---- API ----

  async function api(method, path, body) {
    const opts = { method, headers: {}, credentials: "same-origin" };
    if (method !== "GET") opts.headers["X-Noted"] = "1";
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    let res;
    try {
      res = await fetch(path, opts);
    } catch (e) {
      throw { status: 0, message: "网络出错，请稍后再试" };
    }
    let data = {};
    try { data = await res.json(); } catch (e) { /* empty body */ }
    if (!res.ok) throw { status: res.status, message: data.error || "" };
    return data;
  }

  // Server messages are English; show them in Chinese.
  function zh(err, ctx) {
    const m = (err && err.message) || "";
    const s = err && err.status;
    if (s === 0) return m;
    if (s === 429) return "失败次数过多，请 10 分钟后再试";
    if (/wrong email or password/.test(m)) return "邮箱或密码不正确";
    if (/sign-up is closed/.test(m)) return "这台服务器暂未开放注册";
    if (s === 409) return ctx === "password" ? "该邮箱已被其他账号使用" : "该邮箱已注册，请直接登录";
    if (/password must be/.test(m)) return "密码需为 8 到 128 个字符";
    if (/email/.test(m) && s === 400) return "请输入有效的邮箱地址";
    if (/current password is wrong/.test(m)) return "当前密码不正确";
    if (/current password is required/.test(m)) return "请输入当前密码";
    if (/label is longer/.test(m)) return "名称最多 60 个字符";
    if (s === 401) return "登录已失效，请重新登录";
    return "出错了" + (m ? "：" + m : "，请稍后再试");
  }

  // ---- small UI helpers ----

  let toastTimer;
  function toast(text) {
    const t = $("toast");
    t.textContent = text;
    t.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove("show"), 1800);
  }

  function legacyCopy(text) {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "0";
    ta.style.left = "0";
    ta.style.opacity = "0";
    ta.style.fontSize = "16px"; // no zoom on iOS
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, text.length);
    let ok = false;
    try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }

  function selectNode(node) {
    const r = document.createRange();
    r.selectNodeContents(node);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(r);
  }

  async function copyFrom(node, btn) {
    const text = node.textContent;
    let ok = false;
    if (navigator.clipboard && window.isSecureContext) {
      try { await navigator.clipboard.writeText(text); ok = true; } catch (e) { ok = false; }
    }
    if (!ok) ok = legacyCopy(text);
    if (!ok) {
      selectNode(node);
      toast("已选中，请手动复制");
      return;
    }
    toast("已复制");
    if (btn) {
      const label = btn.dataset.orig || btn.textContent;
      btn.dataset.orig = label;
      btn.textContent = "已复制";
      btn.classList.add("done");
      clearTimeout(btn._t);
      btn._t = setTimeout(() => { btn.textContent = label; btn.classList.remove("done"); }, 1600);
    }
  }

  function fmt(ts) {
    if (!ts) return "";
    const d = new Date(ts);
    if (isNaN(d)) return "";
    const p2 = (n) => String(n).padStart(2, "0");
    const year = d.getFullYear() !== new Date().getFullYear() ? d.getFullYear() + "年" : "";
    return year + (d.getMonth() + 1) + "月" + d.getDate() + "日 " + p2(d.getHours()) + ":" + p2(d.getMinutes());
  }

  function openDialog(d) {
    if (typeof d.showModal === "function") {
      if (!d.open) d.showModal();
    } else {
      d.setAttribute("open", "");
    }
  }
  function closeDialog(d) {
    if (typeof d.close === "function") d.close();
    else d.removeAttribute("open");
  }

  // confirmBox resolves true when the destructive button is pressed.
  function confirmBox(title, msg, yes) {
    const d = $("confirm-dlg");
    $("confirm-title").textContent = title;
    $("confirm-msg").textContent = msg;
    $("confirm-yes").textContent = yes;
    return new Promise((resolve) => {
      const done = (v) => {
        $("confirm-yes").onclick = null;
        $("confirm-no").onclick = null;
        d.onclose = null;
        closeDialog(d);
        resolve(v);
      };
      $("confirm-yes").onclick = () => done(true);
      $("confirm-no").onclick = () => done(false);
      d.onclose = () => done(false);
      openDialog(d);
      $("confirm-no").focus();
    });
  }

  // ---- views ----

  function setSignedIn(account) {
    me = account;
    $("bar-out").hidden = !!me;
    $("bar-in").hidden = !me;
    const go = $("ai-go");
    if (me) {
      const name = me.email || me.user_name || "";
      $("bar-avatar").textContent = name.charAt(0).toUpperCase();
      go.textContent = "去生成令牌";
    } else {
      go.textContent = "注册并生成令牌";
    }
  }

  function showView(which) {
    $("intro-view").hidden = which !== "intro";
    $("account-view").hidden = which !== "account";
    document.querySelectorAll(".nav a").forEach((a) => { a.hidden = which === "account"; });
  }

  // route shows the view the address asks for: #account is the account
  // area, anything else is the product page (and #ai etc. are anchors in it).
  function route() {
    const h = location.hash;
    if (h === "#account") {
      if (!me) {
        showView("intro");
        openAuth("login");
        return;
      }
      showView("account");
      window.scrollTo(0, 0);
      $("acct-h").focus({ preventScroll: true });
      loadTokens();
      return;
    }
    showView("intro");
    if (h === "#intro") {
      window.scrollTo(0, 0);
    } else if (h.length > 1) {
      const el = document.getElementById(h.slice(1));
      if (el) el.scrollIntoView();
    }
  }

  function goAccount() {
    if (location.hash === "#account") route();
    else location.hash = "#account";
  }

  // ---- sign in / sign up ----

  function setMode(m) {
    mode = m;
    const login = m === "login";
    $("tab-login").setAttribute("aria-selected", String(login));
    $("tab-signup").setAttribute("aria-selected", String(!login));
    $("auth-title").textContent = login ? "登录 Noted" : "注册 Noted";
    $("auth-sub").textContent = login ? "用注册时的邮箱和密码登录。" : "用邮箱注册，马上就能给 AI 助手生成令牌。";
    $("auth-submit").textContent = login ? "登录" : "注册";
    $("auth-password").setAttribute("autocomplete", login ? "current-password" : "new-password");
    $("auth-switch-t").textContent = login ? "还没有账号？" : "已经有账号？";
    $("auth-switch").textContent = login ? "注册" : "登录";
    $("auth-err").textContent = "";
  }

  function openAuth(m) {
    if (me) { goAccount(); return; }
    setMode(m);
    openDialog($("auth-dlg"));
    $("auth-email").focus();
  }

  async function submitAuth(ev) {
    ev.preventDefault();
    const email = $("auth-email").value.trim();
    const password = $("auth-password").value;
    $("auth-err").textContent = "";
    if (!email || email.indexOf("@") < 1) { $("auth-err").textContent = "请输入有效的邮箱地址"; $("auth-email").focus(); return; }
    if (password.length < 8) { $("auth-err").textContent = "密码至少 8 位"; $("auth-password").focus(); return; }
    const btn = $("auth-submit");
    btn.disabled = true;
    try {
      await api("POST", mode === "login" ? "/api/login" : "/api/signup", { email, password });
      $("auth-password").value = "";
      await loadMe();
      closeDialog($("auth-dlg"));
      toast(mode === "login" ? "已登录" : "注册成功");
      goAccount();
    } catch (e) {
      $("auth-err").textContent = zh(e, mode);
    } finally {
      btn.disabled = false;
    }
  }

  // ---- account ----

  async function loadMe() {
    const a = await api("GET", "/api/me");
    if (a.mcp_url) mcpURL = a.mcp_url;
    const email = a.email || a.user_name;
    $("acct-email").textContent = email;
    $("info-email").textContent = email;
    $("pw-user").value = a.email || "";
    $("mcp-url").textContent = mcpURL;
    $("snip-claude-oauth").textContent = "claude mcp add --scope user --transport http noted " + mcpURL;
    setSignedIn(a);
  }

  function emptyRow(text) {
    const p = document.createElement("div");
    p.className = "empty";
    p.textContent = text;
    return p;
  }

  async function loadTokens() {
    const list = $("tokens");
    let data;
    try {
      data = await api("GET", "/api/tokens");
    } catch (e) {
      if (e.status === 401) return signedOut();
      list.replaceChildren(emptyRow(zh(e)));
      return;
    }
    const tokens = data.tokens || [];
    $("tok-count").textContent = tokens.length ? tokens.length + " 个" : "";
    if (tokens.length === 0) {
      list.replaceChildren(emptyRow("还没有令牌"));
      return;
    }
    const rows = tokens.map((t) => {
      const row = document.createElement("div");
      row.className = "item";
      const main = document.createElement("div");
      main.className = "main";
      const name = document.createElement("div");
      name.className = "name";
      name.textContent = t.label || "(未命名)";
      if (t.current) {
        const b = document.createElement("span");
        b.className = "badge";
        b.textContent = "当前网页";
        name.appendChild(b);
      }
      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = fmt(t.create_time) + " 创建 · " + (t.last_used_time ? "最近使用 " + fmt(t.last_used_time) : "从未使用");
      main.append(name, meta);
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "revoke";
      btn.textContent = "吊销";
      btn.setAttribute("aria-label", "吊销 " + (t.label || "未命名"));
      btn.addEventListener("click", () => revoke(t));
      row.append(main, btn);
      return row;
    });
    list.replaceChildren(...rows);
  }

  async function revoke(t) {
    const ok = t.current
      ? await confirmBox("吊销当前网页的令牌？", "这是这个网页的登录令牌，吊销后会退出登录。", "吊销并退出")
      : await confirmBox("吊销「" + (t.label || "未命名") + "」？", "用这个令牌的助手会立即断开。", "吊销");
    if (!ok) return;
    try {
      await api("DELETE", "/api/tokens/" + encodeURIComponent(t.id));
      if (t.current) return signedOut();
      if (t.id === shownTokenID) hideResult();
      toast("已吊销");
      await loadTokens();
    } catch (e) {
      if (e.status === 401) return signedOut();
      toast(zh(e));
    }
  }

  async function createToken(ev) {
    ev.preventDefault();
    $("new-err").textContent = "";
    const label = $("new-label").value.trim();
    const btn = $("new-submit");
    btn.disabled = true;
    try {
      const res = await api("POST", "/api/tokens", { label });
      const tok = res.token;
      $("new-name").textContent = (res.info && res.info.label) || label || "未命名";
      $("tok").textContent = tok;
      $("snip-claude").textContent =
        "claude mcp add --scope user --transport http noted " + mcpURL + ' --header "Authorization: Bearer ' + tok + '"';
      $("snip-codex").textContent =
        '[mcp_servers.noted]\nurl = "' + mcpURL + '"\nbearer_token_env_var = "NOTED_TOKEN"';
      $("snip-env").textContent = "export NOTED_TOKEN=" + tok;
      $("new-result").hidden = false;
      shownTokenID = (res.info && res.info.id) || "";
      $("new-label").value = "";
      await loadTokens();
      $("new-result").scrollIntoView({ behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start" });
    } catch (e) {
      if (e.status === 401) return signedOut();
      $("new-err").textContent = zh(e);
    } finally {
      btn.disabled = false;
    }
  }

  async function changePassword(ev) {
    ev.preventDefault();
    $("pw-err").textContent = "";
    $("pw-ok").textContent = "";
    const cur = $("pw-current").value;
    const pw = $("pw-new").value;
    if (!cur) { $("pw-err").textContent = "请输入当前密码"; return; }
    if (pw.length < 8) { $("pw-err").textContent = "新密码至少 8 位"; return; }
    if (pw !== $("pw-again").value) { $("pw-err").textContent = "两次输入的新密码不一致"; return; }
    const btn = $("pw-submit");
    btn.disabled = true;
    btn.textContent = "正在保存…";
    try {
      await api("POST", "/api/password", { current_password: cur, password: pw });
      const u = $("pw-user").value;
      $("pw-form").reset();
      $("pw-user").value = u;
      $("pw-ok").textContent = "密码已更新";
    } catch (e) {
      if (e.status === 401) return signedOut();
      $("pw-err").textContent = zh(e, "password");
    } finally {
      btn.disabled = false;
      btn.textContent = "保存新密码";
    }
  }

  async function logout() {
    try { await api("POST", "/api/logout"); } catch (e) { /* signed out anyway */ }
    signedOut();
    toast("已退出登录");
  }

  function hideResult() {
    ["tok", "snip-claude", "snip-codex", "snip-env", "new-name"].forEach((id) => { $(id).textContent = ""; });
    $("new-result").hidden = true;
    shownTokenID = "";
  }

  function signedOut() {
    hideResult();
    $("tokens").replaceChildren();
    setSignedIn(null);
    if (location.hash) history.replaceState(null, "", location.pathname);
    showView("intro");
    window.scrollTo(0, 0);
  }

  // ---- wiring ----

  document.addEventListener("DOMContentLoaded", async () => {
    document.querySelectorAll("[data-auth]").forEach((b) => {
      b.addEventListener("click", () => openAuth(b.getAttribute("data-auth")));
    });
    document.querySelectorAll("[data-copy]").forEach((b) => {
      b.addEventListener("click", () => copyFrom($(b.getAttribute("data-copy")), b));
    });
    document.querySelectorAll("[data-label]").forEach((b) => {
      if (!b.classList.contains("chip")) return;
      b.addEventListener("click", () => { $("new-label").value = b.getAttribute("data-label"); $("new-label").focus(); });
    });
    $("tab-login").addEventListener("click", () => setMode("login"));
    $("tab-signup").addEventListener("click", () => setMode("signup"));
    $("auth-switch").addEventListener("click", () => setMode(mode === "login" ? "signup" : "login"));
    $("auth-form").addEventListener("submit", submitAuth);
    $("auth-dlg").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) closeDialog(ev.currentTarget); });
    $("new-form").addEventListener("submit", createToken);
    $("pw-form").addEventListener("submit", changePassword);
    $("logout").addEventListener("click", logout);
    window.addEventListener("hashchange", route);

    try {
      await loadMe();
    } catch (e) {
      setSignedIn(null);
    }
    if (me && !location.hash) history.replaceState(null, "", "#account");
    route();
  });
})();
