# Regenerate the agent-facing maps. Hand-written guidance lives in AGENTS.md.
.PHONY: codemap reference test explore explore-quick

codemap:
	go run ./cmd/codemap
	go run ./cmd/charmrefs

# The generated reference (docs/reference): settings and default key rows of
# the framework runtime. TestReference (cmd/docgen) fails when it is stale.
reference:
	go run ./cmd/docgen -ref docs/reference

# The nested modules (cmd/o-story: o-story, the demo app and its stories;
# examples) require ochazuke and monogatari; locally they build against
# the sibling checkouts through the untracked go.work
# (go work use . ./cmd/o-story ./examples ../monogatari ../oden).
NESTED := cmd/o-story examples

# Per-package timeout for the root module's -race run. Go's 10 m default is too
# short: the root package took 650 s under -race on vps2 (204 s plain).
# Override: make test TEST_TIMEOUT=30m.
TEST_TIMEOUT ?= 60m

# test is the gate: gofmt, vet and test of the root module and of every
# nested module. The root module (root, cli, markdown, internal/demo, ...) runs
# once under -race with no plain run; the nested modules (cmd/o-story,
# examples) run plain, since o-story under -race took 1707 s for no gain. A bare
# go test ./... at the root runs the root module only and fails
# TestNestedModulesRunToo; test sets OCHAZUKE_NESTED_TESTS=1 for it. Run it
# as GOWORK=off make test against the tags (a release's step 3).
test:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	go vet ./...
	OCHAZUKE_NESTED_TESTS=1 go test -race -timeout $(TEST_TIMEOUT) ./...
	for d in $(NESTED); do (cd $$d && go vet ./... && go test ./...) || exit 1; done

# Explorer surveys are not part of go test ./...: their tests carry the
# explore build tag. explore-quick runs them at the small survey budget
# (depth 3, 2000 steps; the examples' TestExplore and TestExploreGaps and
# o-story's explore test, all on monogatari's explore package).
explore-quick:
	for d in $(NESTED); do (cd $$d && go test -tags explore -count=1 -timeout 0 ./...) || exit 1; done

# explore-quick, then the deep state-explorer survey (depth 6, 20000 steps)
# of the demo and the examples, reports in explore-out/
# (OCHAZUKE_EXPLORE_FULL=1 also counts full-keyed states); o-story explore
# surveys the story-built apps.
explore: explore-quick
	cd examples && OCHAZUKE_EXPLORE=deep OCHAZUKE_EXPLORE_OUT=$(CURDIR)/explore-out go test -tags explore -count=1 -timeout 0 -run 'TestExplore$$' -v ./...
	cd cmd/o-story && go run . explore --depth 6 --steps 20000 --out $(CURDIR)/explore-out/stories
