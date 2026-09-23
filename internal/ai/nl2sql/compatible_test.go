package nl2sql

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStripJSONCPreservesStrings(t *testing.T) {
	// URLs contain "//" and must survive comment stripping.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"url":"https://opencode.ai/zen/go/v1"}`, "https://opencode.ai/zen/go/v1"},
		{"double-slash-in-string", `{"url":"https://a/b//c"}`, "https://a/b//c"},
		{"line-comment", "{\n\"url\":\"https://x/y\" // note\n}", "https://x/y"},
		{"block-comment", `{"url":/* c */"https://x/y"}`, "https://x/y"},
		{"comment-with-quotes", "{\"a\":1} // \"} not a real brace", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			if err := json.Unmarshal(stripJSONC([]byte(tc.in)), &got); err != nil {
				t.Fatalf("stripJSONC output not valid JSON: %v\ninput: %s\noutput: %s", err, tc.in, stripJSONC([]byte(tc.in)))
			}
			if tc.want == "" {
				return // only checked that it parses
			}
			if got["url"] != tc.want {
				t.Fatalf("url = %q, want %q", got["url"], tc.want)
			}
		})
	}
}

func TestStripJSONCDropsTrailingCommas(t *testing.T) {
	in := `{"a":[1,2,],"b":3,}` + "\n"
	var got map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &got); err != nil {
		t.Fatalf("trailing commas not stripped: %v\noutput: %s", err, stripJSONC([]byte(in)))
	}
	if len(got["a"].([]any)) != 2 {
		t.Fatalf("a = %v, want 2 elements", got["a"])
	}
}

func TestBaseURLFromOpenCodeConfig(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "v2 schema",
			in:   `{"providers":{"opencode-go":{"settings":{"baseURL":"https://custom.example/v1"}}}}`,
			want: "https://custom.example/v1",
		},
		{
			name: "v1 schema",
			in:   `{"provider":{"openai":{"options":{"baseURL":"https://v1.example/v1"}}}}`,
			want: "https://v1.example/v1",
		},
		{
			name: "prefers opencode-go over openai",
			in: `{"providers":{
				"openai":{"settings":{"baseURL":"https://openai.example/v1"}},
				"opencode-go":{"settings":{"baseURL":"https://go.example/v1"}}
			}}`,
			want: "https://go.example/v1",
		},
		{
			name: "jsonc with comments",
			in: `{
				// custom gateway
				"providers": {
					"opencode-go": { "settings": { "baseURL": "https://gateway.example/v1" } } /* inline */
				}
			}`,
			want: "https://gateway.example/v1",
		},
		{name: "no baseURL", in: `{"default_agent":"swe","plugins":[]}`, want: ""},
		{name: "invalid json", in: `{not json`, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseURLFromOpenCodeConfig([]byte(tc.in)); got != tc.want {
				t.Fatalf("baseURLFromOpenCodeConfig = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectOpenCodeBaseURL(t *testing.T) {
	t.Run("reads opencode.jsonc", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".config", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `{"providers":{"opencode-go":{"settings":{"baseURL":"https://jsonc.example/v1"}}}}`
		if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := detectOpenCodeBaseURL(home); got != "https://jsonc.example/v1" {
			t.Fatalf("got %q, want jsonc baseURL", got)
		}
	})

	t.Run("falls back to opencode.json", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".config", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `{"provider":{"openai":{"options":{"baseURL":"https://v1.example/v1"}}}}`
		if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := detectOpenCodeBaseURL(home); got != "https://v1.example/v1" {
			t.Fatalf("got %q, want v1 baseURL", got)
		}
	})

	t.Run("no config", func(t *testing.T) {
		if got := detectOpenCodeBaseURL(t.TempDir()); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}
