#!/usr/bin/env node
// Run from any directory. Uses installed tools; never installs dependencies.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';

const docs = path.dirname(fileURLToPath(import.meta.url));
const repo = path.dirname(docs);
const scratch = process.env.ARCHITECTURE_SCRATCH || '/tmp/och-doc-maps';
const archify = process.env.ARCHIFY_ROOT || path.join(os.homedir(), '.agents/skills/archify');
const mermaid = process.env.BEAUTIFUL_MERMAID_ROOT || path.join(os.homedir(), 'proj/mtm-mermaid/node_modules/beautiful-mermaid');
const artifacts = path.join(scratch, 'artifacts');
for (const dir of [artifacts, path.join(scratch, 'config'), path.join(scratch, 'state')]) fs.mkdirSync(dir, { recursive: true });
const env = { ...process.env, XDG_CONFIG_HOME: path.join(scratch, 'config'), XDG_STATE_HOME: path.join(scratch, 'state'), GOFLAGS: '-trimpath', GOWORK: 'off' };
function run(args, receipt) {
  const result = spawnSync('nice', [process.execPath, ...args], { cwd: repo, env, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  if (receipt) fs.writeFileSync(path.join(artifacts, receipt), result.stdout || '');
  process.stdout.write(result.stdout || '');
  process.stderr.write(result.stderr || '');
  if (result.status !== 0) throw new Error(`${args[0]} exited ${result.status}: ${result.error || ''}`);
}
const digest = (file) => { const bytes = fs.readFileSync(file); return { sha256: createHash('sha256').update(bytes).digest('hex'), bytes: bytes.length }; };
const cli = path.join(archify, 'bin/archify.mjs');
const spec = path.join(docs, 'architecture-map.json');
const map = path.join(docs, 'architecture-map.html');
const raw = path.join(artifacts, 'architecture-map.raw.html');
run([cli, 'validate', 'architecture', spec, '--quality', 'showcase', '--json'], 'map-validation.json');
run([cli, 'deliver', 'architecture', spec, raw, '--quality', 'showcase', '--json'], 'map-delivery.json');
// The skill owns the viewer. This deterministic customization owns only doc links.
let html = fs.readFileSync(raw, 'utf8');
const nav = `<nav class="project-docs-nav" aria-label="Project docs">
<a href="architecture.html">Architecture guide</a>
<a href="keybinding-explorer.html">Keybinding explorer</a>
<a href="extensibility.html">Extensibility</a>
<a href="relationships/index.html">Relationships</a>
</nav>`;
const css = `.project-docs-nav{display:flex;flex-wrap:wrap;gap:.35rem .5rem;margin-top:.6rem}.project-docs-nav a{font-size:.8rem;padding:.2rem .65rem;border:1px solid currentColor;border-radius:999px;color:inherit;opacity:.85;text-decoration:none}.project-docs-nav a:hover,.project-docs-nav a:focus-visible{opacity:1;text-decoration:underline}`;
const headerRow = /(<h1>[^<]*<\/h1>\s*<\/div>)/;
if (!headerRow.test(html) || !html.includes('</style>')) throw new Error('Archify viewer template changed; review navigation insertion');
html = html.replace(headerRow, '$1\n' + nav);
html = html.replace('</style>', css + '\n</style>');
const candidate = path.join(artifacts, 'architecture-map.navigated.html');
fs.writeFileSync(candidate, html);
run([path.join(archify, 'scripts/check-render-output.mjs'), candidate], 'map-postprocess-validation.json');
fs.copyFileSync(candidate, map);
fs.writeFileSync(path.join(artifacts, 'map-postprocess.json'), JSON.stringify({ specification: digest(spec), delivered: digest(raw), navigated: digest(map), customization: 'project-docs-nav only' }, null, 2) + '\n');

const { renderMermaidSVG, THEMES } = await import(pathToFileURL(path.join(mermaid, 'dist/index.js')).href);
const diagrams = [
  ['objects', 'Ownership', 'Noun definitions, live instances, instance components, and UI or headless adapters.'],
  ['context', 'Key resolution and execution', 'Derived ancestry and context, shared mode, capture and bubbling, then ExecuteAt.'],
  ['frames', 'Pages and modals', 'Page focus and mode retention, modal instance lifetime, resolution boundaries, and rendering.'],
];
const rel = path.join(docs, 'relationships');
for (const [name] of diagrams) fs.writeFileSync(path.join(rel, `${name}.svg`), renderMermaidSVG(fs.readFileSync(path.join(rel, `${name}.mmd`), 'utf8'), (THEMES.solarized || { bg: '#fdf6e3', fg: '#657b83' })));
const { ChromeVisualBrowser, findChrome } = await import(pathToFileURL(path.join(archify, 'bin/visual-check.mjs')).href);
const chrome = findChrome({ env });
if (!chrome) throw new Error('Chrome/Chromium is required for PNG exports; SVGs have been generated');
const browser = new ChromeVisualBrowser(chrome, { env });
const exports = [];
try {
  const session = await browser.sessionPromise;
  await browser.cdp.send('Emulation.setDeviceMetricsOverride', { width: 3840, height: 2160, deviceScaleFactor: 1, mobile: false }, session);
  for (const [name] of diagrams) {
    const svg = path.join(rel, `${name}.svg`);
    const loaded = browser.cdp.waitFor('Page.loadEventFired', session);
    await browser.cdp.send('Page.navigate', { url: pathToFileURL(svg).href }, session);
    await loaded;
    const measured = await browser.cdp.send('Runtime.evaluate', { expression: `document.fonts.ready.then(()=>{const s=document.querySelector('svg'); const w=Math.max(1200,Math.min(3600,s.viewBox.baseVal.width*2));s.style.width=w+'px';s.style.height='auto';const r=s.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,scale:1}})`, awaitPromise: true, returnByValue: true }, session);
    if (measured.exceptionDetails) throw new Error(JSON.stringify(measured.exceptionDetails));
    const clip = measured.result.value;
    const shot = await browser.cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true, clip }, session, 20000);
    const png = path.join(rel, `${name}.png`);
    fs.writeFileSync(png, Buffer.from(shot.data, 'base64'));
    exports.push({ source: `${name}.mmd`, sourceDigest: digest(path.join(rel, `${name}.mmd`)), svg: `${name}.svg`, svgDigest: digest(svg), png: `${name}.png`, pngDigest: digest(png), capture: { viewport: [3840, 2160], clip } });
  }
} finally { await browser.close(); }
fs.writeFileSync(path.join(rel, 'exports.json'), JSON.stringify({ renderer: 'beautiful-mermaid', version: JSON.parse(fs.readFileSync(path.join(mermaid, 'package.json'))).version, theme: 'solarized', evidenceKind: 'automated-browser', visualReview: 'pending', exports }, null, 2) + '\n');
// Advertise only the three exports above, retaining historical filenames.
fs.writeFileSync(path.join(rel, 'index.html'), `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Runtime relationships · Ochazuke</title><style>
:root{color-scheme:light;background:#f4f1ea;color:#242d2a;font-family:"Avenir Next",Avenir,sans-serif}*{box-sizing:border-box}body{margin:0}main{width:90%;max-width:1440px;margin:auto;padding:48px 0}a{color:#315f46;text-underline-offset:4px}a:focus-visible{outline:3px solid #315f46;outline-offset:5px}nav{display:flex;flex-wrap:wrap;gap:16px}h1{font:normal clamp(2.6rem,5vw,5rem)/1.05 Georgia,serif;letter-spacing:-.045em}p{max-width:75ch;line-height:1.7}section{margin-top:48px;border-top:1px solid #c7cebf}figure{margin:0;padding:20px;background:#fdf6e3;border:1px solid #d8dcd2;border-radius:8px}img{display:block;width:100%;height:auto}figcaption{display:flex;flex-wrap:wrap;gap:20px;padding-top:20px}footer{margin-top:48px}
</style></head><body><main><header><p>Ochazuke / diagram library</p><h1>Runtime relationships</h1>
<p>Three diagrams of the current runtime. Each has a Mermaid source, a vector SVG, and a Chromium PNG export in the Solarized theme.</p>
<nav aria-label="Project docs"><a href="../architecture.html">Architecture guide</a><a href="../architecture-map.html">Architecture map</a><a href="../keybinding-explorer.html">Keybinding explorer</a><a href="README.md">Sources and regeneration</a></nav>
<nav aria-label="Diagrams">${diagrams.map(([name, title]) => `<a href="#${name}">${title}</a>`).join('')}</nav></header>
${diagrams.map(([name, title, description]) => `<section id="${name}"><h2>${title}</h2><p>${description}</p><figure><a href="${name}.svg"><img src="${name}.svg" alt="${description}" loading="lazy"></a><figcaption><a href="${name}.svg">Open SVG</a><a href="${name}.png">Download PNG</a><a href="${name}.mmd">Mermaid source</a></figcaption></figure></section>`).join('\n')}
<footer><a href="exports.json">Export evidence</a> · <a href="#top" onclick="window.scrollTo(0,0);return false">Back to top</a></footer></main></body></html>\n`.replace('<main>', '<main id="top">'));
// Check all local href/src paths and fragments in the generated gallery and map.
const links = [];
for (const file of [map, path.join(rel, 'index.html')]) {
  for (const match of fs.readFileSync(file, 'utf8').matchAll(/\b(?:href|src)="([^"]+)"/g)) {
    const url = match[1];
    if (/^(?:[a-z]+:|\/\/)/i.test(url)) continue;
    const [pathname, fragment] = url.split('#');
    const target = pathname ? path.resolve(path.dirname(file), decodeURIComponent(pathname)) : file;
    if (!fs.existsSync(target)) throw new Error(`Missing local link in ${file}: ${url}`);
    if (fragment && /\.html$/.test(target) && !fs.readFileSync(target, 'utf8').includes(`id="${fragment}"`)) throw new Error(`Missing fragment in ${file}: ${url}`);
    links.push(url);
  }
}
console.log(`Validated ${links.length} local href/src references; 3 SVG/PNG pairs generated.`);
run([cli, 'visual-check', map, '--json'], 'map-browser-evidence.json');
