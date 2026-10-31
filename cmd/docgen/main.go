// Command docgen generates the offline reference linked from the
// architecture guide: docs/{actions,bindings,api,transport}.html, and with
// -ref DIR the Markdown reference of the framework runtime (refdoc:
// README.md, settings.md, bindings.md) that TestReference holds to the
// catalog.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/format"
	"go/parser"
	"go/token"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/internal/demo"
)

type action struct {
	ochazuke.Operation
	Origin string
}

type binding struct {
	Noun, Mode, Keys, Request string
}

type symbol struct {
	Name, Kind, Declaration, Comment, Source string
}

type catalog struct {
	Page, Title string
	Actions     []action
	Bindings    []binding
	Symbols     []symbol
}

func main() {
	ref := flag.String("ref", "", "write the Markdown reference (refdoc) into this directory instead of the HTML pages")
	flag.Parse()
	if *ref != "" {
		if err := writeReference(".", *ref); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	origins := map[string]string{}
	r := ochazuke.NewRuntime()
	record := func(origin string) {
		for key := range r.Operations {
			if origins[key] == "" {
				origins[key] = origin
			}
		}
	}
	record("NewRuntime")
	ochazuke.NewModel(r)
	record("NewModel")
	ochazuke.NewPicker(r)
	record("NewPicker")
	r = demo.New().Runtime
	record("demo.New")
	data := catalog{}
	for _, op := range r.Catalog() {
		data.Actions = append(data.Actions, action{op, origins[op.Key()]})
	}
	nouns := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		nouns = append(nouns, name)
	}
	sort.Strings(nouns)
	for _, name := range nouns {
		modes := make([]string, 0, len(r.Nouns[name].Bindings))
		for mode := range r.Nouns[name].Bindings {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		for _, mode := range modes {
			for _, b := range r.Nouns[name].Bindings[mode] {
				request, err := json.MarshalIndent(b.Request, "", "  ")
				if err != nil {
					return err
				}
				label := mode
				if label == "" {
					label = "Any base mode"
				}
				keys := b.Keys
				if keys == "" {
					keys = "<" + b.Pattern + ">"
				}
				if b.When != "" {
					keys += " when " + b.When
				}
				data.Bindings = append(data.Bindings, binding{name, label, keys, string(request)})
			}
		}
	}

	// go/doc keeps exported declarations and their source comments together.
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		return err
	}
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	pkg, err := doc.NewFromFiles(fset, files, "ofam.dev/ochazuke")
	if err != nil {
		return err
	}
	add := func(name, kind, comment string, node ast.Node) error {
		var declaration bytes.Buffer
		if err := format.Node(&declaration, fset, node); err != nil {
			return err
		}
		pos := fset.Position(node.Pos())
		data.Symbols = append(data.Symbols, symbol{name, kind, declaration.String(), comment, "../" + filepath.ToSlash(pos.Filename)})
		return nil
	}
	addValues := func(values []*doc.Value, kind string) error {
		for _, value := range values {
			if err := add(strings.Join(value.Names, ", "), kind, value.Doc, value.Decl); err != nil {
				return err
			}
		}
		return nil
	}
	addFuncs := func(funcs []*doc.Func) error {
		for _, fn := range funcs {
			name := fn.Name
			if fn.Recv != "" {
				name = strings.TrimPrefix(fn.Recv, "*") + "." + name
			}
			if err := add(name, "function / method", fn.Doc, fn.Decl); err != nil {
				return err
			}
		}
		return nil
	}
	if err := addValues(pkg.Consts, "constant"); err != nil {
		return err
	}
	if err := addValues(pkg.Vars, "variable"); err != nil {
		return err
	}
	if err := addFuncs(pkg.Funcs); err != nil {
		return err
	}
	for _, typ := range pkg.Types {
		if err := add(typ.Name, "type", typ.Doc, typ.Decl); err != nil {
			return err
		}
		if err := addValues(typ.Consts, "constant"); err != nil {
			return err
		}
		if err := addValues(typ.Vars, "variable"); err != nil {
			return err
		}
		if err := addFuncs(typ.Funcs); err != nil {
			return err
		}
		if err := addFuncs(typ.Methods); err != nil {
			return err
		}
	}
	sort.Slice(data.Symbols, func(i, j int) bool { return data.Symbols[i].Name < data.Symbols[j].Name })
	t, err := template.New("catalog").Parse(page)
	if err != nil {
		return err
	}
	for _, spec := range []struct{ name, title string }{
		{"actions", "Actions"},
		{"bindings", "Default bindings"},
		{"api", "Go API"},
		{"transport", "CLI, socket & MCP"},
	} {
		data.Page, data.Title = spec.name, spec.title
		var output bytes.Buffer
		if err := t.Execute(&output, data); err != nil {
			return err
		}
		path := "docs/" + spec.name + ".html"
		if err := os.WriteFile(path, output.Bytes(), 0644); err != nil {
			return err
		}
		fmt.Println("Generated", path)
	}
	return nil
}

const page = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Ochazuke · {{.Title}}</title>
<style>
:root{--paper:#f3f0e7;--card:#fcfaf4;--ink:#24382f;--muted:#626960;--line:#cccfc0;--green:#284e3c;--accent:#a63d27}*{box-sizing:border-box}html{scroll-padding-top:24px}body{margin:0;background:var(--paper);color:var(--ink);font:16px/1.65 "Avenir Next",Avenir,"Segoe UI",sans-serif}main,header,footer{max-width:1200px;margin:auto;padding:28px}a{color:var(--green);text-underline-offset:4px}h1,h2{font-family:Palatino,Georgia,serif;font-weight:400;line-height:1.1;letter-spacing:-.035em}h1{font-size:clamp(44px,7vw,80px);margin:24px 0}h2{font-size:38px}h3{font-size:19px;margin:0}nav{display:flex;flex-wrap:wrap;gap:24px}section{padding:40px 0;border-top:1px solid var(--line)}.eyebrow,small{color:var(--muted)}.eyebrow{text-transform:uppercase;letter-spacing:.15em;font-size:12px}.toolbar{padding:24px 0}label{display:block;font-weight:600}input{width:100%;max-width:680px;padding:12px;border:1px solid var(--line);background:var(--card);color:var(--ink);font:inherit}a:focus-visible,input:focus-visible,summary:focus-visible{outline:3px solid var(--accent);outline-offset:4px}.entry{background:var(--card);border:1px solid var(--line);padding:20px;margin:14px 0}.entry>p:last-child{margin-bottom:0}summary{cursor:pointer;overflow-wrap:anywhere}summary small{margin-left:12px}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#e4e9da;padding:16px;font-size:13px}code{font-family:Menlo,Consolas,monospace;font-size:.9em;overflow-wrap:anywhere}table{border-collapse:collapse;width:100%;font-size:14px}th,td{text-align:left;vertical-align:top;padding:10px;border-bottom:1px solid var(--line);overflow-wrap:anywhere}.table-scroll{overflow-x:auto}td pre{margin:0;min-width:220px}th{color:var(--muted)}.count{font-family:Menlo,monospace;font-size:14px;color:var(--muted)}[hidden]{display:none!important}@media(max-width:600px){main,header,footer{padding:20px}.entry{padding:14px}nav{gap:14px}}
#bindings table{min-width:680px}#bindings th:nth-child(-n+3),#bindings td:nth-child(-n+3){white-space:nowrap}
nav a[aria-current="page"]{font-weight:700;text-decoration-thickness:3px}
</style></head><body>
<header><a href="architecture.html">← Architecture field guide</a><p class="eyebrow">Ochazuke / generated reference</p><h1>{{.Title}}</h1><p>Actions and noun bindings come from a fresh <code>demo.New()</code> in <code>internal/demo</code>. Go declarations and comments come from the root <code>ofam.dev/ochazuke</code> package source through Go's standard <code>go/doc</code> library. This is a build-time snapshot, not your running application's customized registry.</p>
<nav aria-label="Catalogs"><a href="actions.html"{{if eq .Page "actions"}} aria-current="page"{{end}}>Actions ({{len .Actions}})</a><a href="bindings.html"{{if eq .Page "bindings"}} aria-current="page"{{end}}>Default bindings ({{len .Bindings}})</a><a href="api.html"{{if eq .Page "api"}} aria-current="page"{{end}}>Go API ({{len .Symbols}})</a><a href="transport.html"{{if eq .Page "transport"}} aria-current="page"{{end}}>CLI, socket &amp; MCP</a></nav>
{{if ne .Page "transport"}}<div class="toolbar"><label for="search">Search {{.Title}}</label><input id="search" type="search" placeholder="Search this catalog" autocomplete="off"><p id="matches" role="status"></p></div>
<noscript>All entries are available below. Search requires JavaScript.</noscript>{{end}}</header>
<main>
{{if eq .Page "actions"}}
<section id="actions"><p class="eyebrow">01 / Runtime operations</p><h2>Actions <span class="count">{{len .Actions}}</span></h2><p>Identifiers use <code>noun.verb</code>; CLI arguments use <code>NOUN VERB</code>. Each stage label names where this generator first observed the operation key: <code>NewRuntime</code>, then <code>NewModel</code> and <code>NewPicker</code>, then a separate <code>demo.New()</code> runtime. These labels do not identify framework versus application ownership: the demo also registers framework components, and an existing key may have a different implementation in the demo. Parameters below are the demo registry's declared schema; descriptions and result labels are verbatim, not inferred defaults. Context and value constraints can still reject a well-typed request.</p>
{{range .Actions}}<details class="entry searchable"><summary><code>{{.Noun}}.{{.Verb}}</code> <small>First observed: {{.Origin}}</small> · {{.Description}}</summary><p>CLI: <code>ochazuke {{.Noun}} {{.Verb}}</code> · Result: <code>{{.Result}}</code></p>{{if .Parameters}}<div class="table-scroll"><table><thead><tr><th>Parameter</th><th>Type</th><th>Required</th><th>Description</th></tr></thead><tbody>{{range $name,$p := .Parameters}}<tr><td><code>{{$name}}</code></td><td>{{$p.Type}}</td><td>{{if $p.Required}}Yes{{else}}No{{end}}</td><td>{{$p.Description}}</td></tr>{{end}}</tbody></table></div>{{else}}<p>No declared parameters.</p>{{end}}</details>{{end}}
<p class="empty" hidden>No matching actions.</p></section>
{{end}}
{{if eq .Page "bindings"}}
<section id="bindings"><p class="eyebrow">02 / Registered noun rows</p><h2>Default bindings <span class="count">{{len .Bindings}}</span></h2><p>Every mode-table row of every noun in the demo, including the editor noun's Readline and Vim tables, its pending modes, and the palette, picker, yank-history and modal nouns. This table lists noun declarations only: it omits per-instance binding rows and is not a flattened effective keymap. Keys can also depend on guards and operation availability. The shared resolver drives live key dispatch, macro playback and effective binding/help listings.</p><p>Resolution first considers capture rows (<code>Binding.Capture</code>, configured as <code>capture</code>) outer-first, app first and the editor fallback last; the first answering level wins. A passthrough base mode skips capture. Ordinary rows then bubble from the focused instance (or its inner completion level) outward to app. An open modal cuts off the instances beneath it and inserts the modal noun before app. The editor noun is a fallback only when an input is on the path; for a focused read-only input it answers immediately after that input instead.</p><p>At each level, instance rows for the active mode (plus unqualified instance rows outside a pending mode) precede noun mode rows (including declared mode inheritance), followed by the noun's any-base-mode rows. Exact keys rank ahead of key classes (<code>&lt;digit&gt;</code>, <code>&lt;alpha&gt;</code>, <code>&lt;printable&gt;</code>, <code>&lt;paste&gt;</code>), then <code>&lt;any&gt;</code>; table order breaks ties. A pending mode excludes any-base-mode rows. During bubbling, an available longer sequence anywhere on the remaining walk can make the resolver wait, keeping the innermost exact match as fallback; an answering key class ends that scan. <code>ctrl+space</code> is Bubble Tea v2's spelling for Ctrl-Space.</p>
<div class="table-scroll"><table><thead><tr><th>Noun</th><th>Mode</th><th>Keys</th><th>Request</th></tr></thead><tbody>{{range .Bindings}}<tr class="searchable"><td><code>{{.Noun}}</code></td><td>{{.Mode}}</td><td><code>{{.Keys}}</code></td><td><pre>{{.Request}}</pre></td></tr>{{end}}</tbody></table></div><p class="empty" hidden>No matching registered bindings.</p></section>
{{end}}
{{if eq .Page "api"}}
<section id="api"><p class="eyebrow">03 / Exported Go API</p><h2>Go API <span class="count">{{len .Symbols}}</span></h2><p>Generated exported types, functions, methods, constants and variables from non-test Go source files directly in the root <code>ofam.dev/ochazuke</code> package. Subpackages such as <code>cli</code>, <code>cli/mcp</code>, <code>markdown</code> and <code>grid</code> are outside this catalog's scope; command entry points under <code>cmd/</code> are not library APIs. Unexported fields and function bodies are omitted. Missing prose means the declaration has no source doc comment.</p><p>Go also provides terminal documentation: run <code>go doc -all .</code> or <code>go doc Runtime.Execute</code> from the repository root. This offline reference needs no <code>godoc</code> server or published module.</p>
{{range .Symbols}}<details class="entry searchable"><summary><code>{{.Name}}</code> <small>{{.Kind}}</small></summary>{{if .Comment}}<pre>{{.Comment}}</pre>{{end}}<pre>{{.Declaration}}</pre><a href="{{.Source}}">Source file</a></details>{{end}}<p class="empty" hidden>No matching Go declarations.</p></section>
{{end}}
{{if eq .Page "transport"}}
<section id="transport"><p class="eyebrow">04 / One request contract</p><h2>CLI, socket &amp; MCP</h2><p>The framework provides a Unix-socket adapter for a running UI and an in-process CLI shell with a stdio MCP adapter. These share runtime operations; they do not all connect to the owner's running UI. The framework does not provide an HTTP transport. Use the <a href="actions.html">action catalog</a> for this demo's operation names and parameter schemas.</p><h3>Live UI socket</h3><p>The <code>cmd/ochazuke</code> demo CLI sends one JSON request per Unix-socket connection. <code>Listen</code> delivers a <code>CallMsg</code> to the application's UI loop, which handles it through <code>Runtime.HandleCall</code> and <code>Execute</code>. Running the demo with <code>run --headless</code> suppresses terminal rendering but keeps this same event loop and socket path.</p><pre>// Simplified JavaScript-shaped request; the wire format is JSON.
{
  verb: "undo",
  noun: "input",
  target: "focused-input",
  args: { steps: 1 }
}</pre><p>With an application running, query its actual registry or invoke an action:</p><pre>go run ./cmd/ochazuke operations list
go run ./cmd/ochazuke input undo --json '{"steps":1}'</pre><p><code>Request</code> and <code>Response</code> in the <a href="api.html">Go API catalog</a> describe the wire envelopes; <code>Call</code>, <code>Listen</code> and <code>CallMsg</code> provide transport integration. The socket directory must be private and owned by the current user; the socket file has mode 0600. Connections have ten-second deadlines and a 1 MiB request scanner limit. See <a href="../README.md#live-cli-control">live CLI setup</a> for socket selection.</p><h3>Headless CLI and stdio MCP</h3><p>Applications using <code>cli.Shell</code> execute requests in their own process through <code>Runtime.HandleCall</code> with <code>CallMsg.Headless</code> set, then settle pending work through the delivery contract. They do not need a live UI socket. The shell's <code>mcp serve</code> runs a stdio MCP server with <code>catalog</code>, <code>describe</code>, <code>run</code> and <code>state</code> tools over that headless runtime. It retains the runtime across calls for history and undo, and does not attach to the owner's screen, selections or modals. Pass explicit targets for operations that need them; UI-dependent operations may be unavailable. The <code>cli</code> and <code>cli/mcp</code> APIs live in their subpackages, outside the root API catalog.</p><h3>Actor attribution</h3><p><code>Actor</code> identifies the request issuer for history and agent policy; it is advisory identity, not authentication. Socket and MCP servers bind identity from their startup context rather than trusting a request's actor or an MCP tool argument. <code>cli.Shell.Actor</code> can supply CLI identity; otherwise the shell reads <code>OCHAZUKE_ACTOR</code>. <code>Shell.NewMCPServer</code> binds an agent identity for the server's lifetime. Socket directory permissions protect local access; actor labels do not establish authenticated user identities.</p></section>
{{end}}</main>
<footer>Regenerate from the repository root: <code>go run ./cmd/docgen</code>. Generated file; edit source registrations/comments or <code>cmd/docgen/main.go</code>, then regenerate. <a href="architecture.html#catalogs">Back to the field guide</a>.</footer>
{{if ne .Page "transport"}}
<script>
const search=document.getElementById('search');
const entries=Array.from(document.querySelectorAll('.searchable'),element=>({element,text:element.textContent.toLowerCase()}));
function filter(){const query=search.value.trim().toLowerCase();let count=0;for(const {element,text} of entries){element.hidden=!text.includes(query);if(!element.hidden)count++}document.getElementById('matches').textContent=count+' of '+entries.length+' entries';for(const section of document.querySelectorAll('main section')){const empty=section.querySelector('.empty');if(empty)empty.hidden=!!section.querySelector('.searchable:not([hidden])')}}
search.addEventListener('input',filter);filter();
</script>{{end}}</body></html>
`
