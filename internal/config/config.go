package config

import (
	"log"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	DashboardAuthPass                           string `env:"WA_APP_AUTH_PASSWORD"`
	GRPCListenAddr                              string `env:"WA_APP_LISTEN_ADDR"`
	DashboardHTTPAddr                           string `env:"WA_APP_DASHBOARD_HTTP_ADDR"`
	DashboardStaticDir                          string `env:"WA_APP_DASHBOARD_STATIC_DIR"`
	DataDir                                     string `env:"WA_APP_DATA_DIR"`
	CommonProxy                                 string `env:"WA_COMMON_PROXY"`
	RegistrationProxyEnabled                    bool   `env:"WA_REGISTRATION_PROXY_ENABLED"`
	RegistrationProxySourceOrder                string `env:"WA_REGISTRATION_PROXY_SOURCE_ORDER"`
	RegistrationProxyFallback                   string `env:"WA_REGISTRATION_PROXY_FALLBACK"`
	RegistrationProxyStickyMinutes              int    `env:"WA_REGISTRATION_PROXY_STICKY_MINUTES"`
	RegistrationProxySourceRetryMax             int    `env:"WA_REGISTRATION_PROXY_SOURCE_RETRY_MAX"`
	RegistrationProxySource1024Enabled          bool   `env:"WA_REGISTRATION_PROXY_SOURCE_1024_ENABLED"`
	RegistrationProxySource1024UsernameTemplate string `env:"WA_REGISTRATION_PROXY_SOURCE_1024_USERNAME_TEMPLATE"`
	RegistrationProxySource1024Password         string `env:"WA_REGISTRATION_PROXY_SOURCE_1024_PASSWORD"`
	BulkRegistrationEnabled                     bool   `env:"WA_BULK_REGISTRATION_ENABLED"`
	BulkRegistrationMaxItems                    int    `env:"WA_BULK_REGISTRATION_MAX_ITEMS"`
	BulkRegistrationConcurrency                 int    `env:"WA_BULK_REGISTRATION_CONCURRENCY"`
	HeroSMSAPIKey                               string `env:"WA_HERO_SMS_API_KEY"`
	PGDSN                                       string `env:"WA_APP_PG_DSN"`
	RedisURL                                    string `env:"WA_APP_REDIS_URL"`
	DeviceProfilesFile                          string `env:"WA_APP_DEVICE_PROFILES_FILE"`
	PlayIntegrityAPIURL                         string `env:"WA_APP_PLAY_INTEGRITY_API_URL"`
	PlayIntegrityAPIToken                       string `env:"WA_APP_PLAY_INTEGRITY_API_TOKEN"`
}

func Load() Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("load wa-app config: %v", err)
	}
	return cfg
}
