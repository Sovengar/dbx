package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

func GetHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home, nil
}

type Config struct {
	Theme        ThemeConfig        `mapstructure:"theme"`
	Keybindings  KeybindingsConfig  `mapstructure:"keybindings"`
	Connections  []ConnectionConfig `mapstructure:"connections"`
	AI           AIConfig           `mapstructure:"ai"`
	Session      SessionConfig      `mapstructure:"session"`
	UI           UIConfig           `mapstructure:"ui"`
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
	Providers map[string]AIProviderConf `mapstructure:"providers"`
}

type AIProviderConf struct {
	APIKeyEnv string `mapstructure:"api_key_env"`
	Model     string `mapstructure:"model"`
}

type SessionConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Dir           string `mapstructure:"dir"`
	RetentionDays int    `mapstructure:"retention_days"`
}

type UIConfig struct {
	StatusBar     bool `mapstructure:"statusbar"`
	StatusBarHelp bool `mapstructure:"statusbar_help"`
	HistorySize   int  `mapstructure:"history_size"`
	PageSize      int  `mapstructure:"page_size"`
}

func Load() (*Config, error) {
	v := viper.New()
	setDefaults(v)

	configDir, err := os.UserConfigDir()
	if err == nil {
		v.SetConfigName("config")
		v.SetConfigType("toml")
		v.AddConfigPath(filepath.Join(configDir, "dbx"))
	}

	v.SetEnvPrefix("DBX")
	v.AutomaticEnv()

	_ = v.ReadInConfig()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("theme.mode", "system")

	v.SetDefault("keybindings.mode", "vim")

	v.SetDefault("ai.provider", "anthropic")

	v.SetDefault("session.enabled", true)
	v.SetDefault("session.retention_days", 30)

	v.SetDefault("ui.statusbar", true)
	v.SetDefault("ui.statusbar_help", true)
	v.SetDefault("ui.history_size", 100)
	v.SetDefault("ui.page_size", 100)
}
