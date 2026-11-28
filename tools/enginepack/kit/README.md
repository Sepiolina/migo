# Mai workflow engine kit

This folder is the YAML workflow engine from MaiService and `migo`, packaged
to be added to another project. It reads the same workflow files and writes
the same Excel and CSV outputs.

- To hand it to a coding agent, give it the whole folder (or the zip) and
  say what to integrate it into. The agent's instructions are in
  `AGENTS.md`.
- To use it yourself, see `engine/README.md` for the Go API and
  `docs/CLI.md` for the `migo` command.
- To check it, run `go test ./...` and `go run ./cmd/migo test ../conformance`
  from `engine/`. `MANIFEST.json` names the source commit.
