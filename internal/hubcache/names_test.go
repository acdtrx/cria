package hubcache

import "testing"

// The tag a GGUF file carries is read off its name, last token first. The cases
// below are the shapes the real cache holds plus the ones that must not be
// mistaken for a tag — a parameter count (30B, A3B, 2.6B) looks close enough to
// matter.
func TestQuantLabelReadsTheTagOffTheFileName(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"LFM2.5-2.6B-Q8_0.gguf", "Q8_0"},
		{"Qwen3-Embedding-0.6B-Q8_0.gguf", "Q8_0"},
		{"gemma-4-26B-A4B-it-qat-UD-Q4_K_XL.gguf", "UD-Q4_K_XL"},
		{"Qwen3.6-35B-A3B-UD-IQ2_M.gguf", "UD-IQ2_M"},
		{"NVIDIA-Nemotron-3.5-Lightning-30B-A3B-UD-Q4_K_XL.gguf", "UD-Q4_K_XL"},
		{"Qwen3-30B-A3B-TQ1_0.gguf", "TQ1_0"},
		{"gpt-oss-20b-MXFP4.gguf", "MXFP4"},
		{"qwen2.5-coder-3b-instruct-q4_k_m.gguf", "q4_k_m"},
		{"Qwen3-235B-UD-Q4_K_XL-00002-of-00003.gguf", "UD-Q4_K_XL"},
		{"Qwen3-30B-A3B-Q4_K_M-imat.gguf", "Q4_K_M"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			label, ok := quantLabel(test.name)
			if !ok {
				t.Fatalf("no quantization read from %q, want %q", test.name, test.label)
			}
			if label != test.label {
				t.Errorf("%q reads as %q, want %q", test.name, label, test.label)
			}
		})
	}
}

// unsloth's UD- is part of the tag, not of the model name: it is the spelling
// its documentation gives and the one llama-server resolves, so the label keeps
// it. Nothing else in front of the type is absorbed.
func TestTheUDPrefixIsPartOfTheTag(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"Qwen3-30B-A3B-UD-Q2_K_XL.gguf", "UD-Q2_K_XL"},
		{"Qwen3-235B-UD-Q4_K_XL-00002-of-00003.gguf", "UD-Q4_K_XL"},
		{"Qwen3-30B-A3B-ud-q2_k_xl.gguf", "ud-q2_k_xl"},
		// Model-name tokens that sit where the prefix does stay model name.
		{"gemma-4-26B-A4B-it-qat-Q4_K_XL.gguf", "Q4_K_XL"},
		{"Qwen3-30B-A3B-BF16.gguf", "BF16"},
		{"Qwen3-30B-A3B-Q4_K_M-imat.gguf", "Q4_K_M"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			label, ok := quantLabel(test.name)
			if !ok {
				t.Fatalf("no quantization read from %q, want %q", test.name, test.label)
			}
			if label != test.label {
				t.Errorf("%q reads as %q, want %q", test.name, label, test.label)
			}
		})
	}
}

// An entry names a quant the way the repo's files spell it. The lookup matches
// that spelling and no other: cria does not normalize provider naming, so a tag
// written differently from the one published is absent, visibly, rather than
// guessed at.
func TestMatchQuantMatchesTheTagExactly(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		quant  string
		want   string
	}{
		{
			name:   "the spelling the repo uses",
			labels: []string{"UD-Q2_K_XL", "Q8_0"},
			quant:  "UD-Q2_K_XL",
			want:   "UD-Q2_K_XL",
		},
		{
			// llama.cpp's own -hf repo:TAG resolution ignores case, so this is
			// the one difference that still finds the item.
			name:   "the same tag in another case",
			labels: []string{"UD-Q2_K_XL", "Q8_0"},
			quant:  "ud-q2_k_xl",
			want:   "UD-Q2_K_XL",
		},
		{
			name:   "the tag without the prefix the files carry",
			labels: []string{"UD-Q2_K_XL", "Q8_0"},
			quant:  "Q2_K_XL",
		},
		{
			name:   "the tag with a prefix the files omit",
			labels: []string{"Q4_K_M", "Q8_0"},
			quant:  "UD-Q4_K_M",
		},
		{
			name:   "each spelling finds its own item",
			labels: []string{"UD-Q4_K_XL", "Q4_K_XL"},
			quant:  "Q4_K_XL",
			want:   "Q4_K_XL",
		},
		{
			name:   "a tag the repo does not publish",
			labels: []string{"UD-Q2_K_XL", "Q8_0"},
			quant:  "Q4_K_M",
		},
		{
			name:   "a projector is never the answer to a quant",
			labels: []string{"mmproj-BF16.gguf"},
			quant:  "BF16",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match, found := MatchQuant(test.labels, test.quant)
			if !found {
				if test.want != "" {
					t.Fatalf("%q matched nothing in %v, want %q", test.quant, test.labels, test.want)
				}
				return
			}
			if test.want == "" {
				t.Fatalf("%q matched %q in %v, want no match", test.quant, test.labels[match], test.labels)
			}
			if test.labels[match] != test.want {
				t.Errorf("%q matched %q in %v, want %q", test.quant, test.labels[match], test.labels, test.want)
			}
		})
	}
}

// A file whose name carries no tag is its own item, named exactly as the Hub
// names the file, extension included — the shape draft and projector files take
// (docs/specs/CACHE.md). Shards still fold, and the series takes the name of
// the file its parts belong to.
func TestFilesWithoutATagAreTheirOwnItem(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"mtp-gemma-4-26B-A4B-it.gguf", "mtp-gemma-4-26B-A4B-it.gguf"},
		{"model.gguf", "model.gguf"},
		{"Qwen3-30B-A3B.gguf", "Qwen3-30B-A3B.gguf"},
		{"draft-00001-of-00002.gguf", "draft.gguf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := quantLabel(test.name); ok {
				t.Fatalf("%q reads as a quantization, want none", test.name)
			}
			if label := itemLabel(test.name); label != test.label {
				t.Errorf("%q becomes item %q, want %q", test.name, label, test.label)
			}
		})
	}
}

// A projector carries a precision token that reads like a quantization, and it is
// not one: it pairs with any quant of its repo. It gets an item of its own, named
// after the file, so it is never folded into a quant's row or a quant's delete.
func TestProjectorsAreNeverQuantItems(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"mmproj-BF16.gguf", "mmproj-BF16.gguf"},
		{"mmproj-F16.gguf", "mmproj-F16.gguf"},
		{"mmproj-model-f16.gguf", "mmproj-model-f16.gguf"},
		{"MMPROJ-BF16.gguf", "MMPROJ-BF16.gguf"},
		{"mmproj-BF16-00001-of-00002.gguf", "mmproj-BF16.gguf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if label := itemLabel(test.name); label != test.label {
				t.Errorf("%q becomes item %q, want %q", test.name, label, test.label)
			}
		})
	}
}

// GGUFItem is the rule internal/hubapi shares to sum the same files off the
// Hub's listing that the walk finds on disk, so it has to answer for a repo's
// non-weight files too — and for the extension in whatever case a repo spells
// it.
func TestGGUFItemAnswersOnlyForGGUFFiles(t *testing.T) {
	tests := []struct {
		name  string
		label string
		gguf  bool
	}{
		{name: "Qwen3-30B-A3B-Q4_K_M.gguf", label: "Q4_K_M", gguf: true},
		{name: "BF16/Qwen3-30B-A3B-BF16-00001-of-00002.gguf", label: "BF16", gguf: true},
		{name: "Qwen3-30B-A3B-Q4_K_M.GGUF", label: "Q4_K_M", gguf: true},
		{name: "mmproj-BF16.gguf", label: "mmproj-BF16.gguf", gguf: true},
		{name: "config.json"},
		{name: "README.md"},
		{name: "model-00001-of-00002.safetensors"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			label, gguf := GGUFItem(test.name)
			if gguf != test.gguf {
				t.Fatalf("%q reads as gguf=%v, want %v", test.name, gguf, test.gguf)
			}
			if label != test.label {
				t.Errorf("%q becomes item %q, want %q", test.name, label, test.label)
			}
		})
	}
}

// The -NNNNN-of-NNNNN suffix says how many parts a file has, so an interrupted
// multi-part download is visible from the names alone.
func TestSeriesCompleteReadsTheShardCountOffTheNames(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  bool
	}{
		{
			name:  "no shards to satisfy",
			names: []string{"Qwen3-30B-A3B-Q4_K_M.gguf", "mmproj-BF16.gguf"},
			want:  true,
		},
		{
			name:  "every shard present",
			names: []string{"model-00001-of-00002.safetensors", "model-00002-of-00002.safetensors"},
			want:  true,
		},
		{
			name:  "one shard short",
			names: []string{"model-00001-of-00003.safetensors", "model-00003-of-00003.safetensors"},
			want:  false,
		},
		{
			name:  "two series, one of them short",
			names: []string{"model-00001-of-00001.safetensors", "audio-00001-of-00002.safetensors"},
			want:  false,
		},
		{
			name:  "two whole series",
			names: []string{"model-00001-of-00001.safetensors", "audio-00001-of-00002.safetensors", "audio-00002-of-00002.safetensors"},
			want:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := seriesComplete(test.names); got != test.want {
				t.Errorf("seriesComplete(%v) is %v, want %v", test.names, got, test.want)
			}
		})
	}
}

// A cache directory names the Hub namespace and the repo id it holds; anything
// else in the hub directory is not a repository.
func TestParseRepoDir(t *testing.T) {
	tests := []struct {
		dir      string
		repoType RepoType
		id       string
		ok       bool
	}{
		{dir: "models--unsloth--Qwen3.8-27B-GGUF", repoType: RepoModel, id: "unsloth/Qwen3.8-27B-GGUF", ok: true},
		{dir: "datasets--allenai--c4", repoType: RepoDataset, id: "allenai/c4", ok: true},
		{dir: "spaces--gradio--hello", repoType: RepoSpace, id: "gradio/hello", ok: true},
		{dir: "models--gpt2", repoType: RepoModel, id: "gpt2", ok: true},
		{dir: ".locks"},
		{dir: "xet"},
		{dir: "models"},
		{dir: "models--"},
	}
	for _, test := range tests {
		t.Run(test.dir, func(t *testing.T) {
			repoType, id, ok := parseRepoDir(test.dir)
			if ok != test.ok {
				t.Fatalf("%q parsed=%v, want %v", test.dir, ok, test.ok)
			}
			if !ok {
				return
			}
			if repoType != test.repoType || id != test.id {
				t.Errorf("%q reads as %q %q, want %q %q", test.dir, repoType, id, test.repoType, test.id)
			}
		})
	}
}
