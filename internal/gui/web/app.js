"use strict";

// Session key: given in the address the window opened with, kept for reloads.
const KEY = (() => {
  const k = new URLSearchParams(location.search).get("k");
  try {
    if (k) sessionStorage.setItem("k", k);
    return k || sessionStorage.getItem("k") || "";
  } catch { return k || ""; }
})();
history.replaceState(null, "", "/");

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: { "X-Key": KEY, ...(body ? { "Content-Type": "application/json" } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  let data = null;
  try { data = await res.json(); } catch { /* not JSON */ }
  if (!res.ok) {
    const err = new Error((data && data.error) || `HTTP ${res.status}`);
    err.data = data || {};
    throw err;
  }
  return data;
}

function toast(msg, isErr) {
  const t = document.createElement("div");
  t.className = "toast" + (isErr ? " err" : "");
  t.textContent = msg;
  $("#toasts").append(t);
  setTimeout(() => t.remove(), isErr ? 9000 : 5000);
}

function ago(ts) {
  if (!ts) return "";
  const d = (Date.now() / 1000 - ts) / 86400;
  if (d < 1) return "today";
  if (d < 2) return "yesterday";
  if (d < 31) return `${Math.floor(d)} days ago`;
  if (d < 365) return `${Math.floor(d / 30)} mo ago`;
  return `${Math.floor(d / 365)} yr ago`;
}

function num(n) {
  if (!n) return "0";
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, "") + "k";
  return String(n);
}

function size(b) {
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + " MB";
  return Math.max(1, Math.round(b / 1024)) + " KB";
}

function confirmBox(title, text, okLabel = "OK") {
  const dlg = $("#confirm-dlg");
  $("#confirm-title").textContent = title;
  $("#confirm-text").textContent = text;
  $("#confirm-yes").textContent = okLabel;
  dlg.showModal();
  return new Promise(resolve => {
    const done = v => { dlg.close(); $("#confirm-yes").onclick = $("#confirm-no").onclick = null; resolve(v); };
    $("#confirm-yes").onclick = () => done(true);
    $("#confirm-no").onclick = () => done(false);
    dlg.oncancel = () => done(false);
  });
}

async function busy(btn, label, fn) {
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = label;
  try { return await fn(); } finally { btn.disabled = false; btn.textContent = old; }
}

function openURL(url) { api("POST", "/api/open", { what: "url", url }).catch(e => toast(e.message, true)); }

/* ---------- Tabs ---------- */

function showTab(name) {
  $$(".tabs button").forEach(b => b.classList.toggle("active", b.dataset.tab === name));
  $$(".tab").forEach(s => { s.hidden = s.id !== "tab-" + name; });
  if (name === "installed") loadInstalled();
  if (name === "setup") loadStatus(true);
}
$$(".tabs button").forEach(b => b.addEventListener("click", () => showTab(b.dataset.tab)));

document.addEventListener("click", e => {
  const a = e.target.closest("[data-url]");
  if (a) { e.preventDefault(); openURL(a.dataset.url); }
});

/* ---------- Status, banners ---------- */

let status = {};
let nml = null;

async function loadStatus(withNML) {
  try {
    const out = await api("GET", "/api/status" + (withNML ? "?nml=1" : ""));
    status = out.status;
    if (out.nml) nml = out.nml;
  } catch (e) { toast(e.message, true); return; }
  renderStatus();
}

function renderStatus() {
  $("#chip-running").hidden = !status.running;
  const chip = $("#chip-nml");
  if (!nml) { chip.textContent = "NML …"; chip.className = "chip"; }
  else if (!nml.installed) { chip.textContent = "NML not installed"; chip.className = "chip bad"; }
  else if (nml.checkError) { chip.textContent = "NML installed"; chip.className = "chip ok"; }
  else if (!nml.upToDate) { chip.textContent = "NML update available"; chip.className = "chip warn"; }
  else { chip.textContent = "NML up to date"; chip.className = "chip ok"; }

  const b = [];
  if (status.gameError) {
    b.push(`<div class="banner warn"><span>${esc(status.gameError)}</span><button class="btn small" data-go="setup">Open Setup</button></div>`);
  } else if (nml && !nml.installed) {
    b.push(`<div class="banner warn"><span>NeoModLoader isn't installed. WorldBox mods need it to load.</span><button class="btn small" data-nml>Install NeoModLoader</button></div>`);
  }
  if (status.appUpdate) {
    b.push(`<div class="banner info"><span>wbmm ${esc(status.appUpdate)} is out.</span><button class="btn small blue" data-url="${esc(status.appUrl)}">Download</button></div>`);
  }
  $("#banners").innerHTML = b.join("");

  $("#version").textContent = status.version || "";
  $("#game-found").textContent = status.gameDir ? "Using " + status.gameDir : (status.gameError || "");
  if (document.activeElement !== $("#game-dir")) $("#game-dir").value = status.gameDirSet || "";
  renderNML();
}

function renderNML() {
  const box = $("#nml-box");
  if (status.gameError) { box.innerHTML = `<span class="muted">Set the game folder first.</span>`; return; }
  if (!nml) { box.textContent = "Checking…"; return; }
  let text, btn = "";
  if (!nml.installed) { text = "Not installed."; btn = "Install NeoModLoader"; }
  else if (nml.checkError) { text = "Installed. Couldn't check for updates: " + nml.checkError; }
  else if (!nml.upToDate) { text = `Installed, but ${nml.latest} is out.`; btn = "Update NeoModLoader"; }
  else { text = `Installed and up to date (${nml.latest}).`; }
  box.innerHTML = `<span>${esc(text)}</span>` +
    (btn ? `<button class="btn" data-nml>${esc(btn)}</button>` : "") +
    (nml.releaseUrl ? `<button class="btn ghost" data-url="${esc(nml.releaseUrl)}">Release notes</button>` : "");
}

document.addEventListener("click", async e => {
  const go = e.target.closest("[data-go]");
  if (go) showTab(go.dataset.go);
  const n = e.target.closest("[data-nml]");
  if (!n) return;
  await busy(n, "Installing…", async () => {
    try {
      const out = await api("POST", "/api/nml");
      toast(`NeoModLoader ${out.installed} installed. Start WorldBox with Experimental Mode on.`);
      await loadStatus(true);
    } catch (err) { toast(err.message, true); }
  });
});

$("#game-form").addEventListener("submit", async e => {
  e.preventDefault();
  try {
    await api("POST", "/api/settings", { gameDir: $("#game-dir").value });
    toast("Game folder saved.");
    await loadStatus(true);
    loadInstalled();
  } catch (err) { toast(err.message, true); }
});

/* ---------- Browse ---------- */

const SORTS = {
  gamebanana: [["popular", "Most downloaded"], ["new", "Newest"], ["updated", "Recently updated"], ["liked", "Most liked"]],
  workshop: [["subs", "Most subscribed"], ["popular", "Trending this week"], ["new", "Newest"], ["updated", "Recently updated"]],
};
const NOTES = {
  gamebanana: "GameBanana mods install straight into your Mods folder.",
  workshop: "Workshop mods are installed by Steam: click Subscribe in Steam, then Subscribe on the page that opens.",
};
const browse = { src: "gamebanana", q: "", sort: "popular", page: 1, cards: [], more: false, seq: 0 };

function setSource(src) {
  browse.src = src;
  $$(".seg button").forEach(b => b.classList.toggle("active", b.dataset.src === src));
  $("#sort").innerHTML = SORTS[src].map(([v, l]) => `<option value="${v}">${l}</option>`).join("");
  $("#sort").value = SORTS[src][0][0];
  $("#src-note").textContent = NOTES[src];
  runBrowse(true);
}
$$(".seg button").forEach(b => b.addEventListener("click", () => setSource(b.dataset.src)));
$("#search").addEventListener("submit", e => { e.preventDefault(); runBrowse(true); });
$("#sort").addEventListener("change", () => runBrowse(true));
$("#more").addEventListener("click", () => { browse.page++; runBrowse(false); });

async function runBrowse(reset) {
  const seq = ++browse.seq;
  const q = $("#q").value.trim();
  // Links go to the source they belong to.
  if (/steamcommunity\.com|^ws:/.test(q) && browse.src !== "workshop") { setSource("workshop"); return; }
  if (/gamebanana\.com/.test(q) && browse.src !== "gamebanana") { setSource("gamebanana"); return; }
  if (reset) {
    browse.page = 1;
    browse.cards = [];
    $("#grid").innerHTML = Array(8).fill('<div class="skeleton"></div>').join("");
  }
  $("#more").hidden = true;
  const params = new URLSearchParams({ src: browse.src, q, sort: $("#sort").value, page: browse.page });
  try {
    const out = await api("GET", "/api/browse?" + params);
    if (seq !== browse.seq) return;
    browse.cards = browse.cards.concat(out.cards);
    browse.more = out.more;
  } catch (e) {
    if (seq !== browse.seq) return;
    toast(e.message, true);
  }
  renderGrid();
}

function renderGrid() {
  const g = $("#grid");
  if (!browse.cards.length) {
    g.innerHTML = `<div class="empty">No mods found.</div>`;
  } else {
    g.innerHTML = browse.cards.map(cardHTML).join("");
  }
  $("#more").hidden = !browse.more;
}

function cardHTML(c, i) {
  const stats = c.source === "gamebanana"
    ? `<span>♥ ${num(c.likes)}</span><span>👁 ${num(c.views)}</span>`
    : `<span>${num(c.subs)} subscribers</span>`;
  let main;
  if (c.isMap) main = `<button class="btn ghost small" data-url="${esc(c.url)}">Map: open page</button>`;
  else if (c.source === "workshop") {
    main = c.installed
      ? `<button class="btn done small" data-steam="${esc(c.id)}" title="Open in Steam">✓ Subscribed</button>`
      : `<button class="btn blue small" data-steam="${esc(c.id)}">Subscribe in Steam</button>`;
  } else {
    main = c.installed
      ? `<button class="btn done small" data-install="${i}">✓ Installed</button>`
      : `<button class="btn small" data-install="${i}">Install</button>`;
  }
  return `<article class="card">
    <div class="thumb">${c.thumb ? `<img loading="lazy" alt="" src="${esc(c.thumb)}">` : ""}</div>
    <div class="body">
      <h3>${esc(c.name)}</h3>
      <div class="meta">${c.author ? "by " + esc(c.author) + " · " : ""}${esc(ago(c.updated))}</div>
      ${c.category ? `<span class="tag">${esc(c.category)}</span>` : ""}
      <div class="stats">${stats}</div>
      <div class="buttons">${main}<button class="btn ghost small" data-url="${esc(c.url)}">Page</button></div>
    </div>
  </article>`;
}

$("#grid").addEventListener("click", async e => {
  const s = e.target.closest("[data-steam]");
  if (s) {
    try {
      await api("POST", "/api/open", { what: "steam", id: s.dataset.steam });
      if (!s.classList.contains("done")) toast("Opened in Steam. Click Subscribe there; Steam downloads it and it shows up under Installed.");
    } catch (err) { toast(err.message, true); }
    return;
  }
  const b = e.target.closest("[data-install]");
  if (!b) return;
  const card = browse.cards[+b.dataset.install];
  await busy(b, "Loading…", () => chooseFile(card.id, card.name, b));
});

/* ---------- GameBanana install ---------- */

async function chooseFile(gbId, name, btn) {
  let files;
  try { files = await api("GET", "/api/gbfiles?id=" + encodeURIComponent(gbId)); }
  catch (e) { toast(e.message, true); return; }
  if (!files.length) { toast("This mod has no files to download.", true); return; }
  if (files.length === 1) { await installFile(gbId, files[0], btn); return; }
  $("#files-title").textContent = name;
  $("#files").innerHTML = files.map((f, i) => `<div class="file">
    <div class="info">
      <div class="name">${esc(f._sFile)}</div>
      <div class="desc">${esc(size(f._nFilesize))} · ${esc(ago(f._tsDateAdded))}${f._sVersion ? " · v" + esc(f._sVersion) : ""} ·
        ${f.clean ? '<span class="av-ok">virus scan passed</span>' : '<span class="av-bad">virus scan not passed</span>'}</div>
      ${f._sDescription ? `<div class="desc">${esc(f._sDescription)}</div>` : ""}
    </div>
    <button class="btn small" data-file="${i}">Install</button>
  </div>`).join("");
  $("#files").onclick = async e => {
    const fb = e.target.closest("[data-file]");
    if (!fb) return;
    await busy(fb, "Installing…", () => installFile(gbId, files[+fb.dataset.file], btn));
    $("#files-dlg").close();
  };
  $("#files-dlg").showModal();
}

async function installFile(gbId, file, btn, force = false) {
  const work = async () => {
    try {
      const out = await api("POST", "/api/install", { gbId: String(gbId), fileId: file._idRow, force });
      toast("Installed " + out.installed.join(", ") + ".");
      const c = browse.cards.find(c => c.source === "gamebanana" && c.id === String(gbId));
      if (c) { c.installed = true; renderGrid(); }
      loadInstalled();
      return true;
    } catch (e) {
      if (e.data.notClean && !force) {
        const ok = await confirmBox("Virus scan not passed", e.message + ". Install it anyway? Only do this if you trust the author.", "Install anyway");
        if (ok) return installFile(gbId, file, btn, true);
        return false;
      }
      toast(e.message, true);
      return false;
    }
  };
  if (btn && btn.isConnected && !btn.disabled) return busy(btn, "Installing…", work);
  if (btn && btn.isConnected) btn.textContent = "Installing…";
  return work();
}

/* ---------- Installed ---------- */

let installed = [];
let updates = {};

async function loadInstalled() {
  try { installed = await api("GET", "/api/installed"); }
  catch (e) { installed = []; $("#list").innerHTML = `<div class="empty">${esc(e.message)}</div>`; $("#count").textContent = ""; return; }
  $("#count").textContent = installed.length ? installed.length : "";
  renderInstalled();
  // Workshop cards follow what Steam has downloaded.
  const subs = new Set(installed.filter(m => m.workshopId).map(m => m.workshopId));
  let changed = false;
  for (const c of browse.cards) {
    if (c.source === "workshop" && !c.installed && subs.has(c.id)) { c.installed = true; changed = true; }
  }
  if (changed) renderGrid();
}

function renderInstalled() {
  const f = $("#filter").value.trim().toLowerCase();
  const mods = installed.filter(m => !f || (m.name + " " + m.author).toLowerCase().includes(f));
  if (!installed.length) {
    $("#list").innerHTML = `<div class="empty">No mods yet. Find some under Browse.</div>`;
    return;
  }
  const local = mods.filter(m => m.source !== "workshop");
  const ws = mods.filter(m => m.source === "workshop");
  let html = "";
  if (local.length) html += `<div class="group-title">Mods folder</div>` + local.map(rowHTML).join("");
  if (ws.length) html += `<div class="group-title">Steam Workshop</div>` + ws.map(rowHTML).join("");
  $("#list").innerHTML = html || `<div class="empty">Nothing matches.</div>`;
}
$("#filter").addEventListener("input", renderInstalled);

function rowHTML(m) {
  const i = installed.indexOf(m);
  const icon = m.hasIcon
    ? `<img class="ico" alt="" src="/api/icon?uid=${encodeURIComponent(m.uid)}&k=${encodeURIComponent(KEY)}">`
    : `<div class="ico blank">${esc((m.name || "?")[0].toUpperCase())}</div>`;
  const label = { gamebanana: "GameBanana", workshop: "Workshop", local: "Added by hand" }[m.source];
  const up = updates[m.uid];
  const acts = [];
  if (up) acts.push(`<button class="btn small" data-update="${i}" title="${esc(up.file._sFile)}">Update</button>`);
  if (m.url) acts.push(`<button class="btn ghost small" data-url="${esc(m.url)}">Page</button>`);
  if (m.uid && !m.problem) acts.push(`<button class="btn ghost small" data-folder="${i}">Folder</button>`);
  if (m.source === "workshop") acts.push(`<button class="btn ghost small" data-steam="${esc(m.workshopId)}">Unsubscribe…</button>`);
  else if (m.uid && !m.problem) acts.push(`<button class="btn ghost small" data-remove="${i}">Remove</button>`);
  return `<div class="row-mod${m.enabled ? "" : " off"}">
    ${icon}
    <div>
      <h3>${esc(m.name)}<span class="ver">${esc(m.version)}</span><span class="src ${m.source}">${label}</span></h3>
      <div class="meta">${m.author ? "by " + esc(m.author) : ""}${m.fileName ? " · " + esc(m.fileName) : ""}</div>
      ${m.problem ? `<div class="problem">${esc(m.problem)}</div>` : ""}
    </div>
    <label class="switch" title="${m.enabled ? "On" : "Off"}"><input type="checkbox" data-toggle="${i}" ${m.enabled ? "checked" : ""} ${m.uid && !m.problem ? "" : "disabled"} aria-label="Load ${esc(m.name)}"><span></span></label>
    <div class="acts">${acts.join("")}</div>
  </div>`;
}

$("#list").addEventListener("change", async e => {
  const t = e.target.closest("[data-toggle]");
  if (!t) return;
  const m = installed[+t.dataset.toggle];
  t.disabled = true;
  try {
    await api("POST", "/api/toggle", { uid: m.uid, enabled: t.checked });
    m.enabled = t.checked;
    t.closest(".row-mod").classList.toggle("off", !m.enabled);
  } catch (err) {
    t.checked = !t.checked;
    toast(err.message, true);
  }
  t.disabled = false;
});

$("#list").addEventListener("click", async e => {
  const el = e.target.closest("button");
  if (!el) return;
  if (el.dataset.steam) {
    try {
      await api("POST", "/api/open", { what: "steam", id: el.dataset.steam });
      toast("Opened in Steam. Click Unsubscribe there and Steam removes the mod.");
    } catch (err) { toast(err.message, true); }
  } else if (el.dataset.folder) {
    api("POST", "/api/open", { what: "folder", uid: installed[+el.dataset.folder].uid }).catch(err => toast(err.message, true));
  } else if (el.dataset.remove) {
    const m = installed[+el.dataset.remove];
    if (!await confirmBox("Remove " + m.name + "?", "Its folder is deleted from your Mods folder.", "Remove")) return;
    try {
      await api("POST", "/api/remove", { uid: m.uid });
      toast("Removed " + m.name + ".");
      const c = browse.cards.find(c => c.source === "gamebanana" && c.id === String(m.gbId));
      if (c) { c.installed = false; renderGrid(); }
      loadInstalled();
    } catch (err) { toast(err.message, true); }
  } else if (el.dataset.update) {
    const m = installed[+el.dataset.update];
    const up = updates[m.uid];
    if (await installFile(up.gbId, up.file, el)) delete updates[m.uid];
    renderInstalled();
  }
});

async function checkUpdates(quiet) {
  try {
    const out = await api("GET", "/api/updates");
    updates = {};
    for (const u of out.updates) updates[u.uid] = u;
    const n = out.updates.length;
    if (out.warning && !quiet) toast("Some mods couldn't be checked: " + out.warning, true);
    if (n) toast(`${n} update${n > 1 ? "s" : ""} available under Installed.`);
    else if (!quiet) toast("Everything from GameBanana is up to date. Steam keeps Workshop mods updated.");
    renderInstalled();
  } catch (err) { if (!quiet) toast(err.message, true); }
}
$("#check-updates").addEventListener("click", e => busy(e.currentTarget, "Checking…", () => checkUpdates(false)));

$("#open-mods").addEventListener("click", () => api("POST", "/api/open", { what: "folder" }).catch(e => toast(e.message, true)));

/* ---------- Start ---------- */

function ping() { api("POST", "/api/ping").catch(() => {}); }
ping();
setInterval(ping, 20000);
setInterval(() => loadStatus(false), 10000);
// Back from Steam: pick up newly subscribed Workshop mods.
window.addEventListener("focus", () => { loadInstalled(); loadStatus(false); });

setSource("gamebanana");
loadStatus(true);
loadInstalled().then(() => checkUpdates(true));
