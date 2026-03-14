package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Profile struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SecurityToken   string `json:"security_token,omitempty"`
	Region          string `json:"region,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

type Config struct {
	Version        int                `json:"version"`
	CurrentProfile string             `json:"current_profile,omitempty"`
	Profiles       map[string]Profile `json:"profiles,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Version:  1,
		Profiles: map[string]Profile{},
	}
}

func DefaultConfigPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("TLSCTL_CONFIG")); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tlsctl", "config.json"), nil
}

func Load() (Config, string, error) {
	p, err := DefaultConfigPath()
	if err != nil {
		return Config{}, "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			return cfg, p, nil
		}
		return Config{}, "", err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, "", err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return cfg, p, nil
}

func Save(cfg Config, path string) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) GetProfile(name string) (Profile, bool) {
	if c.Profiles == nil {
		return Profile{}, false
	}
	p, ok := c.Profiles[name]
	return p, ok
}

func (c *Config) PutProfile(name string, p Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}

func EffectiveProfile(cfg Config, name string) (Profile, error) {
	envAK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
	envSK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
	envToken := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
	envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION"))
	envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))
	if envAK != "" && envSK != "" {
		return normalize(Profile{
			AccessKeyID:     envAK,
			SecretAccessKey: envSK,
			SecurityToken:   envToken,
			Region:          envRegion,
			Endpoint:        envEndpoint,
		})
	}

	profileName := strings.TrimSpace(name)
	if profileName == "" {
		profileName = strings.TrimSpace(cfg.CurrentProfile)
	}
	if profileName == "" {
		profileName = "default"
	}

	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return Profile{}, errors.New("profile not found: " + profileName)
	}
	return normalize(p)
}

func normalize(p Profile) (Profile, error) {
	p.AccessKeyID = strings.TrimSpace(p.AccessKeyID)
	p.SecretAccessKey = strings.TrimSpace(p.SecretAccessKey)
	p.SecurityToken = strings.TrimSpace(p.SecurityToken)
	p.Region = strings.TrimSpace(p.Region)
	p.Endpoint = strings.TrimSpace(p.Endpoint)
	if p.Region != "" && p.Endpoint == "" {
		p.Endpoint = DefaultEndpointForRegion(p.Region)
	}
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 60
	}
	if p.AccessKeyID == "" || p.SecretAccessKey == "" {
		return Profile{}, errors.New("missing access key id/secret access key")
	}
	if p.Region == "" {
		return Profile{}, errors.New("missing region")
	}
	if p.Endpoint == "" {
		return Profile{}, errors.New("missing endpoint")
	}
	return p, nil
}

func DefaultEndpointForRegion(region string) string {
	r := strings.TrimSpace(region)
	if r == "" {
		return ""
	}
	if strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") {
		return r
	}
	return "https://tls-" + r + ".volces.com"
}

func MaskAK(ak string) string {
	s := strings.TrimSpace(ak)
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "****" + s[len(s)-3:]
}
