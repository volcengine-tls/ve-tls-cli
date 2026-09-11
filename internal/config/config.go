package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

const (
	AuthModeAK           = "ak"
	AuthModeSSO          = "sso"
	AuthModeConsoleLogin = "console-login"
	AuthModeRamRoleARN   = "ramrolearn"
	AuthModeOIDC         = "oidc"
	AuthModeECSRole      = "ecsrole"
)

type Credential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type Profile struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SecurityToken   string `json:"security_token,omitempty"`
	Region          string `json:"region,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	CredRef         string `json:"cred_ref,omitempty"`
	Mode            string `json:"mode,omitempty"`
	SSOSessionName  string `json:"sso-session-name,omitempty"`
	AccountID       string `json:"account-id,omitempty"`
	RoleName        string `json:"role-name,omitempty"`
	LoginSession    string `json:"login-session,omitempty"`
	STSExpiration   int64  `json:"sts-expiration,omitempty"`
	OIDCTokenFile   string `json:"oidc-token-file,omitempty"`
	RoleTRN         string `json:"role-trn,omitempty"`
	DisableSSL      bool   `json:"disable-ssl,omitempty"`
}

type SSOSession struct {
	Name               string   `json:"name"`
	StartURL           string   `json:"start-url"`
	Region             string   `json:"region"`
	RegistrationScopes []string `json:"registration-scopes,omitempty"`
}

type Config struct {
	Version        int                   `json:"version"`
	CurrentProfile string                `json:"current_profile,omitempty"`
	Profiles       map[string]Profile    `json:"profiles,omitempty"`
	Creds          map[string]Credential `json:"creds,omitempty"`
	SSOSessions    map[string]SSOSession `json:"sso-session,omitempty"`
}

type ProfileDefaults struct {
	Region         string
	Endpoint       string
	TimeoutSeconds int
}

type CredentialStatus struct {
	AccessKeyID     string
	SecretAccessKey string
	SecurityToken   string
	Source          string
	Mode            string
	Present         bool
	AK              bool
	SK              bool
	Token           bool
}

func DefaultConfig() Config {
	return Config{
		Version:     1,
		Profiles:    map[string]Profile{},
		Creds:       map[string]Credential{},
		SSOSessions: map[string]SSOSession{},
	}
}

func DefaultConfigPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("VOLCLOG_CONFIG")); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".volclog", "config.json"), nil
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
	normalizeConfig(&cfg)
	return cfg, p, nil
}

var updateFile = securestore.UpdateFile

func Save(cfg Config, path string) error {
	normalizeConfig(&cfg)
	return updateFile(path, 0o600, func([]byte) ([]byte, error) {
		return marshalIndentNoEscape(cfg)
	})
}

func Update(path string, fn func(*Config) error) (Config, error) {
	var updated Config
	err := updateFile(path, 0o600, func(current []byte) ([]byte, error) {
		var cfg Config
		if current == nil {
			cfg = DefaultConfig()
		} else if err := json.Unmarshal(current, &cfg); err != nil {
			return nil, err
		}
		normalizeConfig(&cfg)
		if err := fn(&cfg); err != nil {
			return nil, err
		}
		data, err := marshalIndentNoEscape(cfg)
		if err != nil {
			return nil, err
		}
		updated = cfg
		return data, nil
	})
	if err != nil {
		return Config{}, err
	}
	return updated, nil
}

func normalizeConfig(cfg *Config) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if cfg.Creds == nil {
		cfg.Creds = map[string]Credential{}
	}
	if cfg.SSOSessions == nil {
		cfg.SSOSessions = map[string]SSOSession{}
	}
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

func NormalizeAuthMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", AuthModeAK:
		return AuthModeAK, nil
	case AuthModeSSO, AuthModeConsoleLogin:
		return mode, nil
	case AuthModeRamRoleARN, AuthModeOIDC, AuthModeECSRole:
		return mode, nil
	default:
		return "", errors.New("unknown auth mode: " + mode)
	}
}

func IsCachedLoginAuthMode(mode string) bool {
	switch mode {
	case AuthModeSSO, AuthModeConsoleLogin:
		return true
	default:
		return false
	}
}

func IsWorkloadAuthMode(mode string) bool {
	switch mode {
	case AuthModeRamRoleARN, AuthModeOIDC, AuthModeECSRole:
		return true
	default:
		return false
	}
}

func IsProviderAuthMode(mode string) bool {
	return IsCachedLoginAuthMode(mode) || IsWorkloadAuthMode(mode)
}

func (c *Config) SelectedProfileName(explicit string) string {
	if name := strings.TrimSpace(explicit); name != "" {
		return name
	}
	if name := strings.TrimSpace(c.CurrentProfile); name != "" {
		return name
	}
	return "default"
}

func (c *Config) PatchProfile(name string, patch func(*Profile) error) error {
	profile, _ := c.GetProfile(name)
	if err := patch(&profile); err != nil {
		return err
	}
	c.PutProfile(name, profile)
	return nil
}

func (c *Config) GetCred(name string) (Credential, bool) {
	if c.Creds == nil {
		return Credential{}, false
	}
	v, ok := c.Creds[name]
	return v, ok
}

func (c *Config) PutCred(name string, v Credential) {
	if c.Creds == nil {
		c.Creds = map[string]Credential{}
	}
	c.Creds[name] = v
}

func ResolveEnvCredentialStatus() CredentialStatus {
	ak := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
	sk := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
	token := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
	if ak == "" && sk == "" && token == "" {
		return CredentialStatus{}
	}
	return buildCredentialStatus(ak, sk, token, "env")
}

func ResolveProfileCredentialStatus(cfg Config, p Profile) CredentialStatus {
	ak := strings.TrimSpace(p.AccessKeyID)
	sk := strings.TrimSpace(p.SecretAccessKey)
	token := strings.TrimSpace(p.SecurityToken)
	source := "profile_inline"
	if ak == "" || sk == "" {
		source = "profile_missing"
	}
	if credRef := strings.TrimSpace(p.CredRef); credRef != "" {
		source = "profile_cred_ref"
		if cred, ok := cfg.GetCred(credRef); ok {
			if ak == "" {
				ak = strings.TrimSpace(cred.AccessKeyID)
			}
			if sk == "" {
				sk = strings.TrimSpace(cred.SecretAccessKey)
			}
			if ak == "" || sk == "" {
				source = "profile_cred_ref_missing"
			}
		} else {
			source = "profile_cred_ref_missing"
		}
	}
	return buildCredentialStatus(ak, sk, token, source)
}

func EffectiveProfile(cfg Config, name string, defaults ProfileDefaults) (Profile, error) {
	envAK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_ID"))
	envSK := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET"))
	envToken := strings.TrimSpace(os.Getenv("VOLCENGINE_TOKEN"))
	envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION"))
	envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))
	if envAK != "" && envSK != "" {
		return normalize(applyDefaults(Profile{
			AccessKeyID:     envAK,
			SecretAccessKey: envSK,
			SecurityToken:   envToken,
			Region:          envRegion,
			Endpoint:        envEndpoint,
		}, defaults))
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
	if strings.TrimSpace(p.CredRef) != "" {
		credName := strings.TrimSpace(p.CredRef)
		if cred, ok := cfg.GetCred(credName); ok {
			if strings.TrimSpace(p.AccessKeyID) == "" {
				p.AccessKeyID = cred.AccessKeyID
			}
			if strings.TrimSpace(p.SecretAccessKey) == "" {
				p.SecretAccessKey = cred.SecretAccessKey
			}
		} else {
			return Profile{}, errors.New("credential not found: " + credName)
		}
	}
	return normalize(applyDefaults(p, defaults))
}

func normalize(p Profile) (Profile, error) {
	p.AccessKeyID = strings.TrimSpace(p.AccessKeyID)
	p.SecretAccessKey = strings.TrimSpace(p.SecretAccessKey)
	p.SecurityToken = strings.TrimSpace(p.SecurityToken)
	p.Region = strings.TrimSpace(p.Region)
	p.Endpoint = strings.TrimSpace(p.Endpoint)
	p.CredRef = strings.TrimSpace(p.CredRef)
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 60
	}
	if p.Region == "" {
		return Profile{}, errors.New("missing region")
	}
	if p.Endpoint == "" {
		return Profile{}, errors.New("missing endpoint")
	}
	return p, nil
}

func applyDefaults(p Profile, d ProfileDefaults) Profile {
	if strings.TrimSpace(p.Region) == "" && strings.TrimSpace(d.Region) != "" {
		p.Region = strings.TrimSpace(d.Region)
	}
	if strings.TrimSpace(p.Endpoint) == "" && strings.TrimSpace(d.Endpoint) != "" {
		p.Endpoint = strings.TrimSpace(d.Endpoint)
	}
	if p.TimeoutSeconds <= 0 && d.TimeoutSeconds > 0 {
		p.TimeoutSeconds = d.TimeoutSeconds
	}
	return p
}

func buildCredentialStatus(ak, sk, token, source string) CredentialStatus {
	ak = strings.TrimSpace(ak)
	sk = strings.TrimSpace(sk)
	token = strings.TrimSpace(token)
	return CredentialStatus{
		AccessKeyID:     ak,
		SecretAccessKey: sk,
		SecurityToken:   token,
		Source:          source,
		Mode:            credentialMode(token),
		Present:         ak != "" && sk != "",
		AK:              ak != "",
		SK:              sk != "",
		Token:           token != "",
	}
}

func credentialMode(token string) string {
	if strings.TrimSpace(token) != "" {
		return "sts"
	}
	return "aksk"
}

func MaskAK(ak string) string {
	s := strings.TrimSpace(ak)
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "****" + s[len(s)-3:]
}
