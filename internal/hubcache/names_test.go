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
		{"gemma-4-26B-A4B-it-qat-UD-Q4_K_XL.gguf", "Q4_K_XL"},
		{"Qwen3.6-35B-A3B-UD-IQ2_M.gguf", "IQ2_M"},
		{"NVIDIA-Nemotron-3.5-Lightning-30B-A3B-UD-Q4_K_XL.gguf", "Q4_K_XL"},
		{"Qwen3-30B-A3B-TQ1_0.gguf", "TQ1_0"},
		{"gpt-oss-20b-MXFP4.gguf", "MXFP4"},
		{"qwen2.5-coder-3b-instruct-q4_k_m.gguf", "q4_k_m"},
		{"Qwen3-235B-UD-Q4_K_XL-00002-of-00003.gguf", "Q4_K_XL"},
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

// A file whose name carries no tag is its own item, named after the file minus
// its extension — the shape draft and projector files take (docs/specs/CACHE.md).
func TestFilesWithoutATagAreTheirOwnItem(t *testing.T) {
	tests := []struct {
		name  string
		label string
	}{
		{"mtp-gemma-4-26B-A4B-it.gguf", "mtp-gemma-4-26B-A4B-it"},
		{"model.gguf", "model"},
		{"Qwen3-30B-A3B.gguf", "Qwen3-30B-A3B"},
		{"draft-00001-of-00002.gguf", "draft"},
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
		{"mmproj-BF16.gguf", "mmproj-BF16"},
		{"mmproj-F16.gguf", "mmproj-F16"},
		{"mmproj-model-f16.gguf", "mmproj-model-f16"},
		{"MMPROJ-BF16.gguf", "MMPROJ-BF16"},
		{"mmproj-BF16-00001-of-00002.gguf", "mmproj-BF16"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if label := itemLabel(test.name); label != test.label {
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
