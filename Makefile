# Regenerate the agent-facing maps. Hand-written guidance lives in AGENTS.md.
.PHONY: codemap test explore explore-quick

codemap:
	go run ./cmd/codemap
	go run ./cmd/charmrefs

test:
	go test ./...

# Explorer surveys are not part of go test ./...: their tests carry the
# explore build tag. explore-quick runs them at the small survey budget
# (depth 3, 2000 steps; the examples' TestExplore and TestExploreGaps, the
# explore package's slow tests, o-story's explore test).
explore-quick:
	go test -tags explore -count=1 -timeout 0 ./explore/... ./examples/... ./cmd/o-story/...

# explore-quick, then the deep state-explorer survey (depth 6, 20000 steps)
# of the demo and the examples, reports in explore-out/
# (OCHAZUKE_EXPLORE_FULL=1 also counts full-keyed states); o-story explore
# surveys the story-built apps.
explore: explore-quick
	OCHAZUKE_EXPLORE=deep OCHAZUKE_EXPLORE_OUT=$(CURDIR)/explore-out go test -tags explore -count=1 -timeout 0 -run 'TestExplore$$' -v ./examples/...
	go run ./cmd/o-story explore --depth 6 --steps 20000 --out $(CURDIR)/explore-out/stories
