# `clstr` CLI

_Learn distributed systems by building them from scratch._

Progressive challenges to learn distributed systems and other complex systems by implementing them yourself.

## Quick Start

Install:

```console
$ go install github.com/clstr-io/clstr/cmd/clstr@latest
```

Or with Homebrew:

```console
$ brew tap clstr-io/tap
$ brew install clstr-io/tap/clstr
```

See [clstr.io](https://clstr.io/guides/cli/#installation) for version pinning and other installation methods.

Start a challenge:

```console
$ clstr list             # List available challenges
$ clstr init kv-store    # Create challenge in current directory
$ clstr test             # Test your implementation
$ clstr logs             # View interleaved logs from all nodes
$ clstr logs n2 n4       # View logs for specific nodes
$ clstr next             # Advance to the next stage
```

## How it Works

Write code, run tests, get detailed feedback. Progress through stages as you build real systems.

Learn more at [clstr.io](https://clstr.io).
