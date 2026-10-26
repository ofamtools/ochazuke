//go:build ignore

// gen-demo-csv writes the fake orders sheet the demo tape opens. The data is
// deterministic (fixed seed) and obviously invented: names are drawn from
// short lists, emails use example.com, ids are sequential.
//
//	go run docs/vhs/gen-demo-csv.go > docs/vhs/data/orders.csv
package main

import (
	"encoding/csv"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

const rows = 3000

var (
	first    = []string{"Aiko", "Ben", "Chika", "Dev", "Emi", "Finn", "Gus", "Hana", "Ivo", "Juno", "Kai", "Lena", "Mio", "Nils", "Ola", "Pia", "Quin", "Ren", "Sora", "Tomo", "Uma", "Vic", "Wren", "Yuki", "Zed"}
	last     = []string{"Abe", "Brook", "Castle", "Dune", "Ember", "Fern", "Grove", "Hill", "Isle", "Jade", "Kite", "Lark", "Moss", "Nori", "Oak", "Pine", "Quill", "Reed", "Stone", "Tide", "Vale", "Wolfe"}
	cities   = []string{"Osaka", "Lisbon", "Austin", "Kyoto", "Oslo", "Denver", "Porto", "Sapporo", "Leeds", "Halifax", "Tucson", "Nagoya", "Ghent", "Perth"}
	products = []string{"sencha", "genmaicha", "hojicha", "matcha", "gyokuro", "bancha", "kukicha", "umeboshi", "nori", "wasabi", "furikake", "salmon flakes"}
	statuses = []string{"paid", "paid", "paid", "shipped", "shipped", "pending", "refunded", "cancelled"}
)

func main() {
	rng := rand.New(rand.NewPCG(2026, 10))
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"id", "date", "customer", "email", "city", "product", "qty", "unit", "total", "status"})
	for i := 1; i <= rows; i++ {
		f, l := first[rng.IntN(len(first))], last[rng.IntN(len(last))]
		qty := 1 + rng.IntN(9)
		unit := 3 + rng.IntN(4700)
		w.Write([]string{
			fmt.Sprintf("ORD-%05d", i),
			start.AddDate(0, 0, rng.IntN(640)).Format("2006-01-02"),
			f + " " + l,
			strings.ToLower(f+"."+l) + "@example.com",
			cities[rng.IntN(len(cities))],
			products[rng.IntN(len(products))],
			fmt.Sprint(qty),
			fmt.Sprintf("%d.%02d", unit/100, unit%100),
			fmt.Sprintf("%.2f", float64(qty*unit)/100),
			statuses[rng.IntN(len(statuses))],
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
