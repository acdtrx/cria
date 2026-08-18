# cria

A single-binary TUI for managing local LLM serving on one host: start, watch and stop
llama.cpp's `llama-server` and mlx-lm's `mlx_lm.server` (each fetches its own model by
Hub reference on first start), and see and prune the Hugging Face cache quant-by-quant
— including deleting
a single quant from a multi-quant GGUF repo, which the `hf` CLI cannot do. Written in
Go (bubbletea); driven by a plain TOML config tree meant to be edited by humans and
coding agents alike. The same binary is scriptable — `cria start`, `cria stop`,
`cria status --json` and `cria docs` drive and document the tree without the TUI, so an
agent can validate an entry it just wrote. Philosophy in `docs/cria.md`, stack and
reasoning in `docs/TECH-STACK.md`, the shape of the code in `docs/ARCHITECTURE.md`.
