package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Hook struct {
	BeforeSync            []string `mapstructure:"beforeSync"`
	AfterSync             []string `mapstructure:"afterSync"`
	BeforeExport          []string `mapstructure:"beforeExport"`
	AfterExport           []string `mapstructure:"afterExport"`
	BeforeAddTranslations []string `mapstructure:"beforeAddTranslations"`
	AfterAddTranslations  []string `mapstructure:"afterAddTranslations"`
}

type File struct {
	Language string `mapstructure:"language"`
	Format   string `mapstructure:"format"`
	Source   string `mapstructure:"source"`
	Target   string `mapstructure:"target"`
	Hooks    Hook   `mapstructure:"hooks"`
}

type Config struct {
	APIURL string `mapstructure:"apiUrl"`
	APIKey string `mapstructure:"apiKey"`
	Files  []File `mapstructure:"files"`
}

func Load() (*Config, error) {
	viper.SetConfigName("accent")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")

	// Also support YAML and TOML
	viper.SetConfigType("")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("could not read config file: %w\nRun 'accentctl init' to create one", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Env var overrides
	if v := os.Getenv("ACCENT_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("ACCENT_API_URL"); v != "" {
		cfg.APIURL = v
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("apiKey is required in config or ACCENT_API_KEY env var")
	}
	if c.APIURL == "" {
		return fmt.Errorf("apiUrl is required in config or ACCENT_API_URL env var")
	}
	if len(c.Files) == 0 {
		return fmt.Errorf("at least one file entry is required in config")
	}
	return nil
}
