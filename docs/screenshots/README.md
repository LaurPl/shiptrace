# Dashboard screenshots

This directory is for screenshots used in the README and launch post. v0.1 was tagged with screenshots captured against the dogfood-seeded dataset — see `scripts/dogfood-seed/main.go`.

## Regenerating

```sh
# 1. Seed the local dogfood dataset
mkdir -p .shiptrace-dogfood
SHIPTRACE_HOME=$(pwd)/.shiptrace-dogfood go run ./scripts/dogfood-seed

# 2. Build a fresh binary with the embedded React bundle
( cd web && npm install && npm run build )
go build -o /tmp/shiptrace ./cmd/shiptrace

# 3. Serve and screenshot
SHIPTRACE_HOME=$(pwd)/.shiptrace-dogfood /tmp/shiptrace serve &
open http://127.0.0.1:7777/today
open http://127.0.0.1:7777/distribution
open http://127.0.0.1:7777/replan
open http://127.0.0.1:7777/agent-skill
open http://127.0.0.1:7777/provider
```

Save the screenshots as `today.png`, `distribution.png`, `replan.png`, `agent-skill.png`, `provider.png` in this directory.

## Why aren't real screenshots committed?

For two reasons:

1. The dataset is synthetic — reconstructed from git tags, not from a continuous live install. Committing screenshots of synthetic data risks giving the impression we ran the tool for a month before launch.
2. The PNGs would be ~200 KB each, and the dashboard renders consistently from the source — anyone running the steps above gets the same image.

After 2-3 weeks of live personal use (per `docs/launch-post.md`), screenshots of real data should replace the synthetic versions.
