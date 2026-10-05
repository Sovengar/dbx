package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/buble/dbx/internal/ai/nl2sql"
)

func SessionDir() string {
	return filepath.Join(os.TempDir(), "dbx", "sessions")
}

// StateDir is where per-user state lives. With no home directory to be had it falls back
// to the temporary directory, which is the same choice SessionDir makes four lines up —
// and the omission was a bug, not a style: the discarded error left home empty, so the
// path came out RELATIVE (`.local/state/dbx`), and every caller resolved it against
// whatever directory dbx happened to be started in. The query history, which lands here,
// was therefore written into the user's project directory by a launcher with no HOME,
// which is reachable from `env -i`, from a stripped container, and from any service
// manager that scrubs the environment.
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "dbx", "state")
	}
	return filepath.Join(home, ".local", "state", "dbx")
}

func GetHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home, nil
}

type Config struct {
	Theme       ThemeConfig        `mapstructure:"theme"`
	Keybindings KeybindingsConfig  `mapstructure:"keybindings"`
	Connections []ConnectionConfig `mapstructure:"connections"`
	AI          AIConfig           `mapstructure:"ai"`
	Session     SessionConfig      `mapstructure:"session"`
	UI          UIConfig           `mapstructure:"ui"`
	Editor      EditorConfig       `mapstructure:"editor"`
}

type ThemeConfig struct {
	Mode string `mapstructure:"mode"`
}

type ConnectionConfig struct {
	Name        string `mapstructure:"name"`
	Provider    string `mapstructure:"provider"`
	URL         string `mapstructure:"url"`
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	Database    string `mapstructure:"database"`
	User        string `mapstructure:"user"`
	PasswordEnv string `mapstructure:"password_env"`
	ReadOnly    bool   `mapstructure:"read_only"`
}

type AIConfig struct {
	Provider  string                    `mapstructure:"provider"`
	Model     string                    `mapstructure:"model"`
	Providers map[string]AIProviderConf `mapstructure:"providers"`
}

type AIProviderConf struct {
	APIKeyEnv string `mapstructure:"api_key_env"`
	Model     string `mapstructure:"model"`
	// BaseURL is the endpoint for a self-hosted or proxied OpenAI-compatible
	// server. It was missing here while nl2sql.ProviderConfig already had the
	// field and Resolve already read it, so a config-file provider could name a
	// key and a model and nothing else: the only way to reach a self-hosted
	// endpoint was an environment detector. mapstructure drops an unknown key
	// without a word, so writing base_url in the file did nothing at all.
	BaseURL string `mapstructure:"base_url"`
}

// Nl2sqlProviders converts the configured AI providers into the provider table
// nl2sql.Resolve consumes. Shared by the CLI (`dbx ask`) and the TUI ASK pane.
func (a AIConfig) Nl2sqlProviders() map[string]nl2sql.ProviderConfig {
	result := make(map[string]nl2sql.ProviderConfig, len(a.Providers))
	for k, v := range a.Providers {
		result[k] = nl2sql.ProviderConfig{
			APIKeyEnv: v.APIKeyEnv,
			Model:     v.Model,
			BaseURL:   v.BaseURL,
		}
	}
	return result
}

type SessionConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Dir           string `mapstructure:"dir"`
	RetentionDays int    `mapstructure:"retention_days"`
}

type UIConfig struct {
	StatusBar     bool `mapstructure:"statusbar"`
	StatusBarHelp bool `mapstructure:"statusbar_help"`
	PageSize      int  `mapstructure:"page_size"`
	YankMaxRows   int  `mapstructure:"yank_max_rows"`
	NerdFont      bool `mapstructure:"nerd_font"`
	// HistorySize caps how many queries the per-project history keeps, dropping
	// the oldest. The store hardcoded 500 while this defaulted to 100, and the two
	// never met: the knob was declared, given a default and read by nothing.
	HistorySize int `mapstructure:"history_size"`
	// StateDir is the ROOT under which dbx keeps its state: the query history
	// lives at <state_dir>/projects/<project>/query_history.json.
	//
	// It replaces `query_history_path`, which was declared with a default and read
	// by nothing — and whose default was not even the shape of the real path,
	// since the real one is per project. Pointing that key at a single file could
	// never have worked. Renamed rather than reinterpreted: the old key did
	// nothing, so no working setup depends on it, and a key that says "the file"
	// while the code writes a directory tree would be worse than no key.
	StateDir string `mapstructure:"state_dir"`
}

type EditorConfig struct {
	Autocomplete        bool `mapstructure:"autocomplete"`
	AutocompleteTrigger int  `mapstructure:"autocomplete_trigger"`
}

func Load() (*Config, error) {
	v := viper.New()
	setDefaults(v)

	configDir, err := os.UserConfigDir()
	if err == nil {
		configPath := filepath.Join(configDir, "dbx")
		v.SetConfigName("config")
		v.SetConfigType("toml")
		v.AddConfigPath(configPath)

		cfgFile := filepath.Join(configPath, "config.toml")
		if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
			_ = os.MkdirAll(configPath, 0o755)
			_ = v.WriteConfigAs(cfgFile)
		}
	}

	v.SetEnvPrefix("DBX")
	// The dots in a nested key have to become underscores, or viper asks for
	// "DBX_UI.PAGE_SIZE" — a name no shell can produce. This is the second half of
	// making the environment overrides work; BindEnv below is the first.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// BindEnv for every key that has a default, derived from AllKeys rather than
	// written out a second time.
	//
	// Without this, AutomaticEnv is decorative. viper resolves an AutomaticEnv key
	// only when something asks for it with Get, while Unmarshal builds its input
	// from AllSettings — which does not include them. So no DBX_* override ever
	// reached the struct, and every setting here is nested (ui.page_size, ai.model,
	// editor.autocomplete), so it was every one of them.
	//
	// Two things were missing, not one: the binding, and the key replacer above.
	// Measured before either: a file with page_size = 7 and DBX_UI_PAGE_SIZE=13 in
	// the environment loaded 7.
	//
	// Derived from AllKeys because a key with a default and no binding is exactly
	// the half-wiring that drifts: someone adds a setting, writes the SetDefault,
	// and the override silently does nothing.
	for _, key := range v.AllKeys() {
		_ = v.BindEnv(key)
	}

	_ = v.ReadInConfig()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("theme.mode", "system")

	v.SetDefault("ai.provider", "opencode-go")
	v.SetDefault("ai.model", "mimo-v2.5")

	v.SetDefault("session.enabled", true)
	v.SetDefault("session.dir", SessionDir())
	v.SetDefault("session.retention_days", 30)

	v.SetDefault("ui.statusbar", true)
	v.SetDefault("ui.statusbar_help", true)
	v.SetDefault("ui.history_size", 100)
	v.SetDefault("ui.page_size", 100)
	v.SetDefault("ui.yank_max_rows", 10)
	v.SetDefault("ui.state_dir", StateDir())
	v.SetDefault("ui.nerd_font", true)

	v.SetDefault("editor.autocomplete", true)
	v.SetDefault("editor.autocomplete_trigger", 1)
}
