package session

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/fasthttp/session/v2"
	"github.com/fasthttp/session/v2/providers/redis"
	"github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"

	"github.com/authelia/authelia/v4/internal/configuration/schema"
	"github.com/authelia/authelia/v4/internal/logging"
	"github.com/authelia/authelia/v4/internal/utils"
)

// NewProviderConfig creates a configuration for creating the session provider.
func NewProviderConfig(config schema.SessionConfiguration, certPool *x509.CertPool) ProviderConfig {
	c := session.NewDefaultConfig()

	c.SessionIDGeneratorFunc = func() []byte {
		bytes := make([]byte, 32)

		_, _ = rand.Read(bytes)

		for i, b := range bytes {
			bytes[i] = randomSessionChars[b%byte(len(randomSessionChars))]
		}

		return bytes
	}

	// Override the cookie name.
	c.CookieName = config.Name

	// Set the cookie to the given domain.
	c.Domain = config.Domain

	// Set the cookie SameSite option.
	switch config.SameSite {
	case "strict":
		c.CookieSameSite = fasthttp.CookieSameSiteStrictMode
	case "none":
		c.CookieSameSite = fasthttp.CookieSameSiteNoneMode
	case "lax":
		c.CookieSameSite = fasthttp.CookieSameSiteLaxMode
	default:
		c.CookieSameSite = fasthttp.CookieSameSiteLaxMode
	}

	// Only serve the header over HTTPS.
	c.Secure = true

	// Ignore the error as it will be handled by validator.
	c.Expiration = config.Expiration

	c.IsSecureFunc = func(*fasthttp.RequestCtx) bool {
		return true
	}

	var redisConfig *redis.Config

	var redisSentinelConfig *redis.FailoverConfig

	var providerName string

	// If redis configuration is provided, then use the redis provider.
	switch {
	case config.Redis != nil:
		serializer := NewEncryptingSerializer(config.Secret)

		var tlsConfig *tls.Config

		if config.Redis.TLS != nil {
			tlsConfig = utils.NewTLSConfig(config.Redis.TLS, tls.VersionTLS12, certPool)
		}

		if config.Redis.HighAvailability != nil && config.Redis.HighAvailability.SentinelName != "" {
			var addrs []string

			if config.Redis.Host != "" {
				addrs = append(addrs, fmt.Sprintf("%s:%d", strings.ToLower(config.Redis.Host), config.Redis.Port))
			}

			for _, node := range config.Redis.HighAvailability.Nodes {
				addr := fmt.Sprintf("%s:%d", strings.ToLower(node.Host), node.Port)
				if !utils.IsStringInSlice(addr, addrs) {
					addrs = append(addrs, addr)
				}
			}

			providerName = "redis-sentinel"
			redisSentinelConfig = &redis.FailoverConfig{
				Logger:           logging.LoggerCtxPrintf(logrus.TraceLevel),
				MasterName:       config.Redis.HighAvailability.SentinelName,
				SentinelAddrs:    addrs,
				SentinelUsername: config.Redis.HighAvailability.SentinelUsername,
				SentinelPassword: config.Redis.HighAvailability.SentinelPassword,
				RouteByLatency:   config.Redis.HighAvailability.RouteByLatency,
				RouteRandomly:    config.Redis.HighAvailability.RouteRandomly,
				Username:         config.Redis.Username,
				Password:         config.Redis.Password,
				DB:               config.Redis.DatabaseIndex, // DB is the fasthttp/session property for the Redis DB Index.
				PoolSize:         config.Redis.MaximumActiveConnections,
				MinIdleConns:     config.Redis.MinimumIdleConnections,
				IdleTimeout:      300,
				TLSConfig:        tlsConfig,
				KeyPrefix:        "authelia-session",
			}
		} else {
			providerName = "redis"
			network := "tcp"

			var addr string

			if config.Redis.Port == 0 {
				network = "unix"
				addr = config.Redis.Host
			} else {
				addr = fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port)
			}

			redisConfig = &redis.Config{
				Logger:       logging.LoggerCtxPrintf(logrus.TraceLevel),
				Network:      network,
				Addr:         addr,
				Username:     config.Redis.Username,
				Password:     config.Redis.Password,
				DB:           config.Redis.DatabaseIndex, // DB is the fasthttp/session property for the Redis DB Index.
				PoolSize:     config.Redis.MaximumActiveConnections,
				MinIdleConns: config.Redis.MinimumIdleConnections,
				IdleTimeout:  300,
				TLSConfig:    tlsConfig,
				KeyPrefix:    "authelia-session",
			}
		}

		c.EncodeFunc = serializer.Encode
		c.DecodeFunc = serializer.Decode
	default:
		providerName = "memory"
	}

	return ProviderConfig{
		c,
		redisConfig,
		redisSentinelConfig,
		providerName,
	}
}
