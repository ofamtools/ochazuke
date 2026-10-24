# Regenerate the agent-facing maps. Hand-written guidance lives in AGENTS.md.
.PHONY: codemap test

codemap:
	go run ./cmd/codemap
	go run ./cmd/charmrefs

test:
	go test ./...
