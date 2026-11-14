package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	DBPath             string
	BaseURL            string
	SiteName           string
	Tagline            string
	SecureCookies      bool
	AllowSetup         bool
	OpenRegistration   bool
	TrustProxy         bool
	RemoteImages       bool
	DMBlocklist        []string
	SessionTTL         time.Duration
	ImageModeration    bool
	ImageNSFWThreshold float64
	ImageGoreThreshold float64
}

func Load() Config {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("TS_BASE_URL")), "/")
	secure, err := strconv.ParseBool(env("TS_SECURE_COOKIES", "false"))
	if err != nil {
		secure = strings.HasPrefix(base, "https://")
	}
	allowSetup, err := strconv.ParseBool(env("TS_ALLOW_SETUP", "true"))
	if err != nil {
		allowSetup = true
	}
	openRegistration, err := strconv.ParseBool(env("TS_OPEN_REGISTRATION", "false"))
	if err != nil {
		openRegistration = false
	}
	trustProxy, err := strconv.ParseBool(env("TS_TRUST_PROXY", "false"))
	if err != nil {
		trustProxy = false
	}
	remoteImages, err := strconv.ParseBool(env("TS_ALLOW_REMOTE_IMAGES", "false"))
	if err != nil {
		remoteImages = false
	}
	imageModeration, err := strconv.ParseBool(env("TS_IMAGE_MODERATION", "true"))
	if err != nil {
		imageModeration = true
	}
	return Config{
		Addr:               env("TS_ADDR", ":8090"),
		DBPath:             env("TS_DB", "townsquare.db"),
		BaseURL:            base,
		SiteName:           env("TS_SITE_NAME", "Townsquare"),
		Tagline:            env("TS_TAGLINE", "a small, self-hosted place to publish things"),
		SecureCookies:      secure,
		AllowSetup:         allowSetup,
		OpenRegistration:   openRegistration,
		TrustProxy:         trustProxy,
		RemoteImages:       remoteImages,
		DMBlocklist:        listEnv("TS_DM_BLOCKLIST"),
		SessionTTL:         30 * 24 * time.Hour,
		ImageModeration:    imageModeration,
		ImageNSFWThreshold: floatEnv("TS_IMAGE_NSFW_THRESHOLD", 0.60),
		ImageGoreThreshold: floatEnv("TS_IMAGE_GORE_THRESHOLD", 0.60),
	}
}

func floatEnv(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil || v <= 0 || v >= 1 {
		return fallback
	}
	return v
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func listEnv(key string) []string {
	v := env(key, "")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
