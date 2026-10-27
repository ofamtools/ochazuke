# Regenerate the agent-facing maps. Hand-written guidance lives in AGENTS.md.
.PHONY: codemap test explore

codemap:
	go run ./cmd/codemap
	go run ./cmd/charmrefs

test:
	go test ./...

# The deep state-explorer survey (depth 6, 20000 steps) of the demo and the
# examples, reports in explore-out/ (OCHAZUKE_EXPLORE_FULL=1 also counts
# full-keyed states); o-story explore surveys the story-built apps.
explore:
	OCHAZUKE_EXPLORE=deep OCHAZUKE_EXPLORE_OUT=$(CURDIR)/explore-out go test -count=1 -timeout 0 -run TestExplore -v ./examples/...
	go run ./cmd/o-story explore --depth 6 --steps 20000 --out $(CURDIR)/explore-out/stories
