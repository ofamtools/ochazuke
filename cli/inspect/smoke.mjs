// Headless smoke test for a page catalog inspect wrote: loads it from
// file:// in Chrome over the DevTools protocol, fails on console errors or
// exceptions, checks the host's first noun, drives the keyboard through
// every view, and writes screenshots.
//
//   node cli/inspect/smoke.mjs PAGE.html [screenshot-dir]
//
// A page to run it on: OCHAZUKE_INSPECT_OUT=/tmp/demo.html go test
// ./examples/demo -run TestCatalogInspect. CHROME names the browser.
import { spawn } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

if (!process.argv[2]) { console.error("usage: node smoke.mjs PAGE.html [screenshot-dir]"); process.exit(2); }
const page = resolve(process.argv[2]);
const shots = resolve(process.argv[3] || join(tmpdir(), "catalog-inspector-shots"));
mkdirSync(shots, { recursive: true });
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const profile = mkdtempSync(join(tmpdir(), "ci-chrome-"));
const port = 9300 + Math.floor(Math.random() * 500);
const proc = spawn(chrome, ["--headless=new", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
  "--no-first-run", "--no-default-browser-check", "--window-size=1600,1000", "about:blank"], { stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let failures = [];
const check = (ok, what) => { console.log((ok ? "ok   " : "FAIL ") + what); if (!ok) failures.push(what); };

async function targets() {
  for (let i = 0; i < 50; i++) {
    try { const r = await fetch(`http://127.0.0.1:${port}/json`); return await r.json(); } catch { await sleep(200); }
  }
  throw new Error("chrome did not start");
}

try {
  const list = await targets();
  const ws = new WebSocket(list.find((t) => t.type === "page").webSocketDebuggerUrl);
  await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
  let id = 0; const pending = new Map(); const errors = [];
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); return; }
    if (msg.method === "Runtime.exceptionThrown") errors.push("exception: " + JSON.stringify(msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text));
    if (msg.method === "Runtime.consoleAPICalled" && (msg.params.type === "error" || msg.params.type === "warning")) errors.push("console." + msg.params.type + ": " + msg.params.args.map((a) => a.value ?? a.description).join(" "));
    if (msg.method === "Log.entryAdded" && msg.params.entry.level === "error") errors.push("log: " + msg.params.entry.text);
  };
  const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
  const evaluate = async (expr) => (await send("Runtime.evaluate", { expression: expr, returnByValue: true })).result?.result?.value;
  const key = async (k) => {
    const map = { Enter: 13, Escape: 27 };
    const p = { key: k, text: k.length === 1 ? k : undefined, windowsVirtualKeyCode: map[k] || k.toUpperCase().charCodeAt(0) };
    await send("Input.dispatchKeyEvent", { type: "keyDown", ...p });
    await send("Input.dispatchKeyEvent", { type: "keyUp", ...p });
    await sleep(60);
  };
  const type = async (s) => { for (const ch of s) await send("Input.insertText", { text: ch }); await sleep(100); };
  const shot = async (name) => {
    const r = await send("Page.captureScreenshot", { format: "png" });
    const f = join(shots, name + ".png"); writeFileSync(f, Buffer.from(r.result.data, "base64")); console.log("     screenshot " + f);
  };

  await send("Runtime.enable"); await send("Log.enable"); await send("Page.enable");
  await send("Emulation.setDeviceMetricsOverride", { width: 1600, height: 1000, deviceScaleFactor: 1, mobile: false });
  const t0 = Date.now();
  await send("Page.navigate", { url: "file://" + page });
  for (let i = 0; i < 50 && (await evaluate("document.readyState")) !== "complete"; i++) await sleep(100);
  await sleep(400);
  console.log(`     loaded in ${Date.now() - t0} ms`);

  const nv = await evaluate(`D.verbs.length`);
  check(nv > 0, `inlined data parsed (${nv} verbs)`);
  const host = await evaluate(`D.host`);
  const selTool = await evaluate(`document.querySelector('#l-tools .row.sel .n')?.textContent`);
  check(!!host && selTool === host, `tool pane starts on the host (${host}, got ${selTool})`);
  const selNoun = await evaluate(`document.querySelector('#l-nouns .row.sel .n')?.textContent.trim()`);
  check(!!selNoun, `noun pane starts on the host's first noun (${selNoun})`);
  const verbs = await evaluate(`[...document.querySelectorAll('#l-verbs .row .n')].map(e=>e.textContent.trim().split(' ')[0])`);
  check(Array.isArray(verbs) && verbs.length > 0, `its verbs listed (${(verbs || []).length}: ${(verbs || []).slice(0, 6).join(", ")}…)`);
  const det = await evaluate(`document.querySelector('#d-nouns h2')?.textContent`);
  check(!!det, `noun detail drawn (${det})`);
  await shot("1-by-noun");

  await key("l"); // into verbs
  await key("j");
  const vd = await evaluate(`document.querySelector('#d-nouns h2')?.textContent`);
  check((vd || "").startsWith(selNoun + "."), `l/j move into a ${selNoun} verb's detail (${vd})`);
  await shot("2-verb-detail");

  // Search for one of the host's own verbs by noun and verb words.
  const target = await evaluate(`(() => { const v = D.verbs.find((v) => v.noun.startsWith(D.host + ":") && !v.builtin && !v.user); return v ? [v.noun.slice(D.host.length + 1), v.name] : null; })()`);
  if (target) {
    await key("/"); await type(target.join(" ")); await key("Enter");
    const sv = await evaluate(`[...document.querySelectorAll('#l-verbs .row .n')].map(e=>e.textContent.trim().split(' ')[0])`);
    check((sv || [])[0] === target[1], `search "${target.join(" ")}" puts ${target[1]} first (${(sv || []).slice(0, 3)})`);
    await key("Escape"); await key("Escape"); await key("Escape"); await key("Escape");
    const q = await evaluate(`document.querySelector('#q').value`);
    check(q === "", "esc clears the search");
  }

  const annotated = await evaluate(`!!D.annotations`);
  const gapsShown = await evaluate(`getComputedStyle(document.querySelector('#tab-gaps')).display !== 'none'`);
  check(gapsShown === annotated, `Gaps tab ${annotated ? "shown with" : "hidden without"} annotations`);
  const views = [["2", "#l-keys", "by-key", "v-keys", true], ["3", "#l-rel", "relations", "v-rel", false]];
  if (annotated) views.push(["4", "#l-gaps", "gaps", "v-gaps", true]);
  for (const [k, list, name, main, needRows] of views) {
    await key(k); await sleep(150);
    const on = await evaluate(`document.querySelector('main.on').id`);
    const n = await evaluate(`document.querySelectorAll('${list} .row').length`);
    check(on === main, `view ${k} ${name} is shown`);
    if (needRows) check(n > 0, `view ${k} ${name} renders rows (${n} drawn)`);
    await key("j"); await key("j");
    await shot("3-" + name);
  }
  if (annotated) {
    await key("4");
    const gh = await evaluate(`document.querySelector('#d-gaps').innerText.slice(0, 300)`);
    check(/written 2\d{3}-\d\d-\d\d/.test(gh || ""), "gaps view shows the annotations' date");
    // Each suggestion row of the selected noun says present, partial or missing.
    const st = await evaluate(`[...document.querySelectorAll('#d-gaps table.t tr')].slice(1).map((r) => r.cells[0].textContent.trim())`);
    check((st || []).length > 0 && st.every((x) => /^(present|missing|verb, no )/.test(x)), `gaps marks each suggestion (${(st || []).slice(0, 4)})`);
  }
  await key("2"); await key("Enter"); await sleep(150);
  const back = await evaluate(`document.querySelector('main.on').id`);
  check(back === "v-nouns", "enter on a key row opens its verb in By noun");
  await key("?"); await sleep(50);
  check(await evaluate(`document.querySelector('#help').classList.contains('on')`), "? opens help");
  await key("Escape");

  // Dark scheme render.
  await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
  await key("1"); await sleep(100); await shot("4-dark");

  check(errors.length === 0, `no console errors (${errors.length})${errors.length ? ": " + errors.slice(0, 5).join(" | ") : ""}`);
  ws.close();
} catch (e) {
  failures.push(String(e)); console.log("FAIL " + e);
} finally {
  proc.kill();
  await sleep(300);
  rmSync(profile, { recursive: true, force: true });
}
console.log(failures.length ? `${failures.length} failure(s)` : "all checks passed");
process.exit(failures.length ? 1 : 0);
