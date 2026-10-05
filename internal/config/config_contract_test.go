package config

// The contracts of the configuration layer: where things go on disk, and what a config
// file is allowed to say.
//
// Two surfaces, and they fail differently:
//
//	D1  the directories. Absolute or bust — a relative path writes user state into
//	    whatever directory the process happened to start in
//	D2  Load: the defaults, the file it scaffolds, and the fact that it must never
//	    overwrite a config the user already wrote
//	D3  Nl2sqlProviders, the translation from the file's shape to the shape the
//	    resolver consumes — where a field that exists on one side and not the other
//	    is a silent dead knob
//
// Everything here runs against a temp home and a temp config dir, so a test never reads
// or writes the developer's own.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"

	"github.com/buble/dbx/internal/ai/nl2sql"
)

// hasField reports whether a struct type declares a field with the given name.
func hasField(t reflect.Type, name string) bool {
	for i := range t.NumField() {
		if t.Field(i).Name == name {
			return true
		}
	}
	return false
}

// Scenario: Los directorios son ABSOLUTOS o no son nada.
//
// A relative path is not a smaller version of an absolute one — it names a different
// place, chosen by whoever launched the process. This is not hypothetical: dbx is a TUI
// that people start from a project directory, and its per-user state is the query
// history, which is a list of every statement they have run.
func TestTheStateDirectoriesAreAbsolute(t *testing.T) {
	t.Run("with a home directory they sit under it", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		if got := StateDir(); got != filepath.Join(home, ".local", "state", "dbx") {
			t.Errorf("StateDir is %q, want it under the home directory", got)
		}
		if got := SessionDir(); got != filepath.Join(os.TempDir(), "dbx", "sessions") {
			t.Errorf("SessionDir is %q, want it under the temp directory", got)
		}
	})

	t.Run("with NO home directory they fall back to the temp directory", func(t *testing.T) {
		// The defect: StateDir discarded the error from os.UserHomeDir, so with
		// an empty home it returned ".local/state/dbx" — RELATIVE. Measured: the
		// query history, whose default path is built from StateDir, landed in the
		// directory dbx was started from.
		//
		// SessionDir four lines up had always fallen back to TempDir, so the two
		// sisters disagreed about what to do with no home. The same arithmetic
		// written twice and left to drift, which is what this repository keeps
		// producing.
		t.Setenv("HOME", "")
		for _, tc := range []struct {
			name string
			got  string
			want string
		}{
			{"StateDir", StateDir(), filepath.Join(os.TempDir(), "dbx", "state")},
			{"SessionDir", SessionDir(), filepath.Join(os.TempDir(), "dbx", "sessions")},
		} {
			if tc.got != tc.want {
				t.Errorf("%s is %q, want %q", tc.name, tc.got, tc.want)
			}
			if !filepath.IsAbs(tc.got) {
				t.Errorf("%s is %q, which is RELATIVE — it would resolve against the working directory", tc.name, tc.got)
			}
		}
	})

	t.Run("with no home the two fallbacks do not collide", func(t *testing.T) {
		// Sessions and state under the same temporary root is fine; under the
		// SAME directory is not, because one cleanup takes the other with it.
		t.Setenv("HOME", "")
		if StateDir() == SessionDir() {
			t.Errorf("StateDir and SessionDir are both %q — a cleanup of one takes the other", StateDir())
		}
	})

	t.Run("a home directory that is only a slash still gives an absolute path", func(t *testing.T) {
		t.Setenv("HOME", "/")
		if got := StateDir(); !filepath.IsAbs(got) {
			t.Errorf("StateDir is %q with a root home, want an absolute path", got)
		}
	})

	t.Run("the directories are the ones setDefaults promises", func(t *testing.T) {
		// The default for the query history is built from StateDir at setDefaults
		// time, so a change there silently moves where the history is written.
		home := t.TempDir()
		t.Setenv("HOME", home)

		v := viper.New()
		setDefaults(v)

		want := StateDir()
		if got := v.GetString("ui.state_dir"); got != want {
			t.Errorf("the state_dir default is %q, want %q", got, want)
		}
		if got := v.GetString("session.dir"); got != SessionDir() {
			t.Errorf("the session.dir default is %q, want %q", got, SessionDir())
		}
	})

	t.Run("GetHomeDir reports the error instead of swallowing it", func(t *testing.T) {
		// The sibling of StateDir that DOES return an error, and it is the one
		// callers that care should be using. Asserted so the difference is a
		// choice rather than an accident.
		t.Setenv("HOME", t.TempDir())
		home, err := GetHomeDir()
		if err != nil {
			t.Fatalf("GetHomeDir: %v", err)
		}
		if home == "" {
			t.Error("GetHomeDir returned no home and no error")
		}

		t.Setenv("HOME", "")
		if _, err := GetHomeDir(); err == nil {
			t.Error("GetHomeDir with an empty home returned no error")
		}
	})
}

// Scenario: Load entrega los defaults, y NUNCA pisa un config que ya existe.
//
// Load writes a config file as a side effect of reading one. That is the first-run
// scaffolding, and it is also the shape where a bug destroys work: if the scaffold path
// ever runs against an existing file, every knob a user ever set is gone with no
// warning. So the "does not overwrite" half is asserted, not assumed.
func TestLoadDeliversDefaultsWithoutOverwriting(t *testing.T) {
	// withHome points both HOME and the config dir at temporary places, so the
	// test never touches the developer's own config. XDG_CONFIG_HOME is what
	// os.UserConfigDir reads on Linux; the other two cover the platforms that
	// read different variables.
	withHome := func(t *testing.T) (home, configDir string) {
		t.Helper()
		home = t.TempDir()
		configDir = filepath.Join(home, "config")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", configDir)
		t.Setenv("AppData", configDir)
		return home, configDir
	}

	t.Run("with no file anywhere every default is delivered", func(t *testing.T) {
		withHome(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		for _, tc := range []struct {
			name string
			got  any
			want any
		}{
			{"theme.mode", cfg.Theme.Mode, "system"},
			{"ai.provider", cfg.AI.Provider, "opencode-go"},
			{"ai.model", cfg.AI.Model, "mimo-v2.5"},
			{"session.enabled", cfg.Session.Enabled, true},
			{"session.retention_days", cfg.Session.RetentionDays, 30},
			{"ui.statusbar", cfg.UI.StatusBar, true},
			{"ui.statusbar_help", cfg.UI.StatusBarHelp, true},
			{"ui.history_size", cfg.UI.HistorySize, 100},
			{"ui.page_size", cfg.UI.PageSize, 100},
			{"ui.yank_max_rows", cfg.UI.YankMaxRows, 10},
			{"ui.nerd_font", cfg.UI.NerdFont, true},
			{"editor.autocomplete", cfg.Editor.Autocomplete, true},
			{"editor.autocomplete_trigger", cfg.Editor.AutocompleteTrigger, 1},
		} {
			if tc.got != tc.want {
				t.Errorf("the default for %s is %v, want %v", tc.name, tc.got, tc.want)
			}
		}
		if cfg.UI.StateDir == "" {
			t.Error("the state directory default is empty")
		}
		if !filepath.IsAbs(cfg.UI.StateDir) {
			t.Errorf("the state directory default is %q, which is relative", cfg.UI.StateDir)
		}
		if cfg.Session.Dir == "" {
			t.Error("the session directory default is empty")
		}
	})

	t.Run("the first run leaves a config file behind", func(t *testing.T) {
		_, configDir := withHome(t)

		if _, err := Load(); err != nil {
			t.Fatalf("Load: %v", err)
		}
		path := filepath.Join(configDir, "dbx", "config.toml")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Load did not scaffold %s: %v", path, err)
		}
	})

	t.Run("an EXISTING config is read and NOT overwritten", func(t *testing.T) {
		// The whole reason to assert this: the scaffold is a write, and a write
		// aimed at the wrong moment is how a user's settings disappear.
		_, configDir := withHome(t)
		dir := filepath.Join(configDir, "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "config.toml")
		written := `[ui]
page_size = 7

[ai]
model = "my-model"
`
		if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.UI.PageSize != 7 {
			t.Errorf("page_size is %d, want the 7 from the file", cfg.UI.PageSize)
		}
		if cfg.AI.Model != "my-model" {
			t.Errorf("ai.model is %q, want the one from the file", cfg.AI.Model)
		}
		// The untouched keys still get their defaults, which is the other half:
		// a file that sets one thing must not zero everything else.
		if cfg.Theme.Mode != "system" {
			t.Errorf("theme.mode is %q, want the default even though the file says nothing about it", cfg.Theme.Mode)
		}

		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != written {
			t.Errorf("Load rewrote the config file:\\n%s", after)
		}
	})

	t.Run("a malformed config is reported rather than silently defaulted", func(t *testing.T) {
		// viper's ReadInConfig error is discarded on purpose — a file being
		// absent is normal — but Unmarshal's is not, so a config that parses as
		// TOML and does not fit the struct is an error the user can see.
		_, configDir := withHome(t)
		dir := filepath.Join(configDir, "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// page_size must be an integer; a string there cannot decode.
		if err := os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("[ui]\npage_size = \"not a number\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Error("Load accepted a config whose page_size is a string")
		}
	})

	t.Run("a malformed TOML file is tolerated, not fatal", func(t *testing.T) {
		// The asymmetry is deliberate and asserted: ReadInConfig's error is
		// dropped so a broken or absent file still yields the defaults, because a
		// TUI that refuses to start over a typo in an optional file is worse than
		// one that starts with defaults. The Unmarshal error, which means the file
		// WAS read and does not fit, is returned.
		_, configDir := withHome(t)
		dir := filepath.Join(configDir, "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("this is [[[ not toml\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load on an unparseable file: %v", err)
		}
		if cfg.UI.PageSize != 100 {
			t.Errorf("page_size is %d, want the default from a file that could not be read", cfg.UI.PageSize)
		}
	})

	t.Run("the environment overrides the file", func(t *testing.T) {
		// DBX_ prefix, per v.SetEnvPrefix. A user overriding one knob for one run
		// without editing the file they keep in version control.
		_, configDir := withHome(t)
		dir := filepath.Join(configDir, "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("[ui]\npage_size = 7\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DBX_UI_PAGE_SIZE", "13")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.UI.PageSize != 13 {
			t.Errorf("page_size is %d, want the environment's 13 to win over the file's 7", cfg.UI.PageSize)
		}
	})

	t.Run("connections and providers come through whole", func(t *testing.T) {
		// The nested shapes are where a mapstructure tag typo hides: a typo in the
		// tag means the key never lands and the field is silently zero.
		_, configDir := withHome(t)
		dir := filepath.Join(configDir, "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		written := `[[connections]]
name = "prod"
provider = "postgres"
url = "postgres://localhost/prod"
read_only = true
password_env = "PGPASSWORD"

[ai.providers.local]
api_key_env = "LOCAL_KEY"
model = "local-model"
`
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(written), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.Connections) != 1 {
			t.Fatalf("the config holds %d connections, want 1", len(cfg.Connections))
		}
		c := cfg.Connections[0]
		for _, tc := range []struct {
			name      string
			got, want string
		}{
			{"name", c.Name, "prod"},
			{"provider", c.Provider, "postgres"},
			{"url", c.URL, "postgres://localhost/prod"},
			{"password_env", c.PasswordEnv, "PGPASSWORD"},
		} {
			if tc.got != tc.want {
				t.Errorf("connections[0].%s is %q, want %q", tc.name, tc.got, tc.want)
			}
		}
		if !c.ReadOnly {
			t.Error("connections[0].read_only is false, want true")
		}
		// The nested provider map, which is where the second level of tags hides.
		p, ok := cfg.AI.Providers["local"]
		if !ok {
			t.Fatalf("ai.providers holds %v, want a key %q", keysOf(cfg.AI.Providers), "local")
		}
		if p.APIKeyEnv != "LOCAL_KEY" {
			t.Errorf("api_key_env is %q", p.APIKeyEnv)
		}
		if p.Model != "local-model" {
			t.Errorf("the provider model is %q", p.Model)
		}
	})
}

// Scenario: La traduccion de config a proveedor no pierde ni inventa campos.
//
// Nl2sqlProviders is the one place the file's shape becomes the shape the AI resolver
// consumes, so it is where a field that exists on one side and not the other becomes a
// dead knob. There is one such field today and it is worth stating plainly rather than
// leaving to be discovered.
func TestNl2sqlProvidersTranslatesWithoutLosingFields(t *testing.T) {
	t.Run("every configured provider comes across", func(t *testing.T) {
		a := AIConfig{Providers: map[string]AIProviderConf{
			"local":  {APIKeyEnv: "LOCAL_KEY", Model: "local-model"},
			"remote": {APIKeyEnv: "REMOTE_KEY", Model: "remote-model"},
		}}

		got := a.Nl2sqlProviders()
		if len(got) != 2 {
			t.Fatalf("the translation holds %d providers (%v), want 2", len(got), keysOfMap(got))
		}
		if got["local"].APIKeyEnv != "LOCAL_KEY" || got["local"].Model != "local-model" {
			t.Errorf("the local provider came across as %+v", got["local"])
		}
		if got["remote"].APIKeyEnv != "REMOTE_KEY" || got["remote"].Model != "remote-model" {
			t.Errorf("the remote provider came across as %+v", got["remote"])
		}
	})

	t.Run("no providers gives an empty map, not a nil one", func(t *testing.T) {
		// A nil map is rangeable and lenable, so this is not a crash either way;
		// asserted because the returned map is handed to code that may write to
		// it, and writing to a nil map panics.
		got := AIConfig{}.Nl2sqlProviders()
		if got == nil {
			t.Error("the translation returned a nil map")
		}
		if len(got) != 0 {
			t.Errorf("the translation holds %v, want nothing", got)
		}
	})

	t.Run("the provider NAME is the key and survives verbatim", func(t *testing.T) {
		// The name is the only thing tying a config entry to a user-visible
		// provider, and it is the one thing nothing normalises — so a name with
		// odd characters is preserved rather than silently rewritten.
		for _, name := range []string{"a", "my-provider", "My_Provider", "with space", "ünïcødé"} {
			a := AIConfig{Providers: map[string]AIProviderConf{name: {APIKeyEnv: "K"}}}
			got := a.Nl2sqlProviders()
			if _, ok := got[name]; !ok {
				t.Errorf("the provider %q came across as %v, want the name kept verbatim", name, keysOfMap(got))
			}
		}
	})

	t.Run("a provider with no key env still comes across, with an empty key", func(t *testing.T) {
		// Dropping it here would hide the entry entirely; keeping it lets the
		// resolver say "this provider is configured but has no key", which is a
		// far better answer than "you have no providers".
		got := AIConfig{Providers: map[string]AIProviderConf{"bare": {Model: "m"}}}.Nl2sqlProviders()
		if p, ok := got["bare"]; !ok {
			t.Errorf("the provider did not come across; got %v", keysOfMap(got))
		} else if p.APIKeyEnv != "" {
			t.Errorf("the key env is %q, want empty", p.APIKeyEnv)
		}
	})

	t.Run("the translation is a COPY, so editing it does not touch the config", func(t *testing.T) {
		// It builds a fresh map rather than aliasing, which matters because the
		// resolver may annotate what it is given.
		original := map[string]AIProviderConf{"a": {APIKeyEnv: "K", Model: "m"}}
		cfg := AIConfig{Providers: original}
		got := cfg.Nl2sqlProviders()
		got["a"] = nl2sql.ProviderConfig{}
		got["injected"] = nl2sql.ProviderConfig{APIKeyEnv: "NEW"}

		if original["a"].APIKeyEnv != "K" {
			t.Errorf("the config's provider was changed to %+v by editing the translation", original["a"])
		}
		if len(cfg.Nl2sqlProviders()) != 1 {
			t.Error("a provider added to the translation leaked back into the config")
		}
	})

	t.Run("a base URL in the file REACHES the resolver", func(t *testing.T) {
		// This subtest used to assert that a base URL COULD NOT be configured from
		// the file, and to expire by itself when that stopped being true: the field
		// was missing on AIProviderConf while nl2sql.ProviderConfig had one and
		// Resolve already read it, so a config-file provider could name a key and
		// a model and nothing else. A self-hosted or proxied endpoint was reachable
		// only through an environment detector, and mapstructure drops an unknown
		// key without a word, so writing base_url did nothing at all.
		//
		// The field exists now, so this asserts the whole path instead: the tag
		// decodes, and the translation carries it to the type Resolve consumes.
		v := viper.New()
		v.Set("api_key_env", "K")
		v.Set("model", "local-model")
		v.Set("base_url", "http://self-hosted/v1")

		var target AIProviderConf
		if err := v.Unmarshal(&target); err != nil {
			t.Fatal(err)
		}
		if target.BaseURL != "http://self-hosted/v1" {
			t.Errorf("the base_url decoded as %q; check the mapstructure tag", target.BaseURL)
		}

		got := AIConfig{Providers: map[string]AIProviderConf{
			"local": {APIKeyEnv: "K", Model: "local-model", BaseURL: "http://self-hosted/v1"},
		}}.Nl2sqlProviders()
		if got["local"].BaseURL != "http://self-hosted/v1" {
			t.Errorf("the translation carried the base URL as %q", got["local"].BaseURL)
		}

		// And the field is still there on the resolver's side, so there is
		// somewhere for the value to arrive.
		if !hasField(reflect.TypeOf(nl2sql.ProviderConfig{}), "BaseURL") {
			t.Error("nl2sql.ProviderConfig no longer has a BaseURL field")
		}
	})
}

func keysOf(m map[string]AIProviderConf) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// keysOfMap is the key set of a translation result, for error messages.
func keysOfMap[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
