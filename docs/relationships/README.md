# Runtime relationship diagrams

Read these with the [architecture guide](../architecture.html), [interactive map](../architecture-map.html), and [key walkthrough](../keybinding-walkthrough.html#relationships). The [gallery](index.html) lists only the three diagrams actually exported, in the Solarized theme.

- `objects.mmd`: ownership and references. Runtime owns noun and instance registries, operations and shared interaction state; typed components live on instances. The Model is a UI adapter, while CLI/MCP can use a headless loop.
- `context.mmd`: shared key resolution and execution. Context is derived from instance ancestry; it is not a separate layer registry. Capture runs outer first except in passthrough modes, then ordinary rows bubble inner first. The top modal bounds the walk. The editor fallback applies to input paths (read-only inputs have an earlier editor level). Sequences can wait across levels; a key class can stop that scan. `DispatchOutcome` reports handled, consumed or unhandled. Palette and adapter requests enter execution directly.
- `frames.mmd`: page and modal lifetime. The filename is retained for existing links. A page is an instance with a Page component that remembers focus and base mode. Modal bodies are instances, with return focus and mode; `Pop` closes a created subtree through delivery and restores the prior interaction. Model.View draws the current page and overlays modals in order.

These are selected relationships, not exhaustive field inventories or an inheritance hierarchy. `Instance.Component` can hold a typed component; components doing asynchronous work implement the delivery contract. Inputs own text, cursor and undo state; interaction mode belongs to Runtime. Moving focus to another stop applies the nearest declared pin or normal mode. Page changes and modal close restore saved base modes.

`ExecuteAt` canonicalizes requests, resolves targets (including `ResolveTarget`), checks arguments and applicability, runs verb checks and confirmation, then dispatches an action override or operation handler. It records history and triggers watches/hooks. Handlers can return Pending work; ticketed completions return to the owner loop through `Deliver`. Both UI and headless adapters must serialize runtime mutation; headless execution does not require a Model. User messages go through the UI component and the adapter's sink, with call-time messages also captured in Response.Messages.

## Regeneration

Editable sources are `../architecture-map.json` and the three `.mmd` files here. From the repository root, run:

```sh
node docs/generate-architecture.mjs
```

The helper uses the installed [archify skill](https://github.com/tt-a1i/archify) under `$HOME/.agents/skills/archify` and Beautiful Mermaid 1.1.3 from the sibling `mtm-mermaid/node_modules/beautiful-mermaid` directory. It never installs tools. Override `ARCHIFY_ROOT` or `BEAUTIFUL_MERMAID_ROOT` to point at existing installations. Chrome/Chromium is required; `ARCHIFY_CHROME` selects its executable. `ARCHITECTURE_SCRATCH` defaults to `/tmp/och-doc-maps`; XDG config/state and generation receipts live there, not in owner directories. Generation uses Node because both existing renderers are JavaScript tools; no Go runtime changes or builds are involved.

The recipe, in order:

1. Validate and deliver the architecture specification with archify's `showcase` quality profile. Preserve its raw delivery receipt in the scratch artifacts directory.
2. Add the project's documentation navigation through a deterministic postprocess, check the resulting HTML with archify's artifact checker, and record hashes for both raw and final HTML in `map-postprocess.json`. This explicitly distinguishes the skill's delivered bytes from the customized page.
3. Render all three Mermaid sources through Beautiful Mermaid with Solarized colors. Capture the actual SVG elements in headless Chromium at a 3840×2160 viewport, at twice the authored viewBox width (minimum 1200, maximum 3600 CSS pixels), preserving aspect ratio and capturing the whole element. `exports.json` records source/SVG/PNG hashes and actual capture dimensions.
4. Generate the gallery from those three exports, then validate local navigation paths and HTML fragments in both pages. No absent theme variants are advertised.
5. Run archify's `visual-check` against the final customized map. Its checked-in sidecars contain bounded browser measurements, four screenshots, a contact sheet, and an artifact-bound receipt. Automated evidence leaves perceptual `visualReview` pending; any actual image review is reported separately.

The helper exits on failed validation or capture. Generation receipts in scratch include actual command output; an unsuccessful run must not be reported as complete. Browser runs may require the coordinator's browser-capable environment. The helper does not regenerate CODEMAP, reference pages, catalogs, or historical docs.

Source contracts: `runtime.go` (Runtime, Context, ExecuteAt), `noun.go` (definitions and mode tables), `instance.go` (Instance, Payload, Component), `resolve.go` (DispatchOutcome, stack and walk), `focus.go` (focus rule and modal lifetime), `pages.go` (Page and OpenPage), `delivery.go` (tickets and completion), `model.go` (UI adapter and rendering), `ui.go` (messages and sinks), and `transport.go` plus `cli/shell.go` (calls and headless execution).
