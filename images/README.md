# Images

Each subdirectory contains a Dockerfile for a supported language. clstr embeds these at build time via `embed.go` and serves them to users running `clstr init`.

## Requirements

Every Dockerfile must install `iptables` and `iproute2`. clstr uses these to simulate network conditions (latency, partitions, packet loss) between nodes. Without them, network fault injection will not work.

## Adding a language

1. Create a new directory under `images/` named after the language (e.g. `images/python`).
2. Copy `images/template/Dockerfile` as a starting point and fill in the language-specific steps.
3. If you want to support common aliases (e.g. `py` -> `python`), add them to the `aliases` map in `embed.go`.
4. Add a Dependabot entry for the new directory in `.github/dependabot.yaml`, sorted alphabetically within the existing docker entries.

## Updating a language version

Change the `FROM` line in the relevant Dockerfile. Dependabot will open PRs for minor and major version bumps automatically; patch updates are ignored.

When adding a Dependabot entry, note that languages differ in how they version their Docker images. Some use minor versions as the meaningful unit (e.g. `python:3.13`, `go:1.26`, `rust:1.85`, `elixir:1.18`), so patch updates should be ignored. Others use major versions (e.g. `node:22`, `node:24`), so minor updates should be ignored instead. Follow the pattern in the existing entries and adjust the `ignore` block accordingly.
