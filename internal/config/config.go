package config

import (
	"log"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	ListenAddr                   string `env:"WA_APP_LISTEN_ADDR" envDefault:":50091"`
	DashboardHTTPAddr            string `env:"WA_APP_DASHBOARD_HTTP_ADDR" envDefault:":8080"`
	DashboardStaticDir           string `env:"WA_APP_DASHBOARD_STATIC_DIR" envDefault:"/app/dashboard/wa"`
	ProxyRuntimeAPIURL           string `env:"WA_APP_PROXY_RUNTIME_API_BASE_URL"`
	ProxyRuntimeLocalProtocol    string `env:"WA_APP_PROXY_RUNTIME_LOCAL_PROTOCOL" envDefault:"socks5"`
	LongConnectionProxyUsername  string `env:"WA_LONG_CONNECTION_PROXY_USERNAME" envDefault:"whatsapp"`
	NumberProbeProxyUsername     string `env:"WA_NUMBER_PROBE_PROXY_USERNAME" envDefault:"whatsapp-probe"`
	RegistrationProxyUsername    string `env:"WA_REGISTRATION_PROXY_USERNAME" envDefault:"whatsapp-reg"`
	AccountSettingsProxyUsername string `env:"WA_ACCOUNT_SETTINGS_PROXY_USERNAME" envDefault:"whatsapp-reg"`
	LoginStateCheckProxyUsername string `env:"WA_LOGIN_STATE_CHECK_PROXY_USERNAME" envDefault:"whatsapp-reg"`
	PGDSN                        string `env:"WA_APP_PG_DSN"`
	RedisURL                     string `env:"WA_APP_REDIS_URL"`
	DataDir                      string `env:"WA_APP_DATA_DIR" envDefault:"/var/lib/wa-app"`
}

func Load() Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("load wa-app config: %v", err)
	}
	return cfg
}
