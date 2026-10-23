# Runtime relationship diagrams

Read these with [the walkthrough](../keybinding-walkthrough.html#relationships).

- `objects.mmd`: selected fields and references, not inheritance or exhaustive ownership.
- `frames.mmd`: a hypothetical active stack sharing its base Frame with a retained Page.
- `context.mmd`: separate layer and input lookups from a Context.

The `.mmd` files are editable Mermaid sources. SVGs are CLI-rendered with Beautiful Mermaid; PNGs are high-resolution Chromium captures of those SVGs. Keep both exports synchronized when changing a source.

With the Beautiful Mermaid skill installed, render from the repository root:

```sh
for name in objects frames context; do
  bun run "$HOME/.agents/skills/beautiful-mermaid/scripts/render.ts" \
    --input "docs/relationships/$name.mmd" \
    --output "docs/relationships/$name" --theme solarized
done
```

For PNG export, open each SVG in Chromium, scale its SVG element to twice its viewBox width (at least 1200 CSS pixels), preserve the aspect ratio, and capture the SVG element as PNG. The walkthrough uses the vector SVGs; PNGs are provided for sharing.

Source contracts: `model.go` (`Model`), `runtime.go` (`Runtime`, `Frame`, `Context`, `Layer`, `ExecuteAt`, `EffectiveBindings`), and `pages.go` (`Page`, `OpenPage`). Modal is a Frame's active-stack role; registry is an ID-keyed map, not a separate core type.
