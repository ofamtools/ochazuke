# README demo

`demo.tape` records `assets/demo.gif`, the animation under the README header. It drives the CSV example (`examples/csv`) over `data/orders.csv`: 3,000 rows of invented orders.

Regenerate from the repository root (needs [VHS](https://github.com/charmbracelet/vhs) and Go; `brew install vhs`):

```sh
vhs docs/vhs/demo.tape
```

The tape builds the example into a temporary directory off camera, so the recording shows only the app. It takes about a minute and writes a GIF of roughly 17 seconds and under 2 MB.

To regenerate the data (deterministic, fixed seed):

```sh
go run docs/vhs/gen-demo-csv.go > docs/vhs/data/orders.csv
```

## Reusing it in another project

Copy `demo.tape`, change the `Hide` block to build and start that project's app, and replace the key steps. Keep `Output assets/demo.gif` so the README header needs no change. To stay under 2 MB, keep the window small (`Width`, `Height`), the run short, and the pauses few; `Framerate` matters less, because VHS drops unchanged frames.
