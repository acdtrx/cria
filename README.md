# cria

A single-binary TUI for managing local LLM serving on one host: start, watch and stop
llama.cpp's `llama-server` and mlx-lm's `mlx_lm.server`, download models through the
`hf` CLI, and see and prune the Hugging Face cache quant-by-quant — including deleting
a single quant from a multi-quant GGUF repo, which the `hf` CLI cannot do. Written in
Go (bubbletea); driven by a plain TOML config tree meant to be edited by humans and
coding agents alike. Philosophy in `docs/cria.md`, stack and reasoning in
`docs/TECH-STACK.md`.
