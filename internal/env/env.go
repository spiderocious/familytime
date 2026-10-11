// Package env parses the environment once, at boot. A malformed environment
// fails loudly here rather than surfacing three layers deep.
package env

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Env struct {
	AppEnv      string
	Port        int
	CORSOrigins []string
	MongoURI    string
	MongoDB     string
	// Where the web app lives. Join links and QR codes are built from this.
	AppURL   string
	LogLevel string
}

const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// Load reads .env (if present) and then the process environment. Real
// environment variables win over the file.
func Load() (Env, error) {
	loadDotEnv(".env")

	port, err := strconv.Atoi(get("PORT", "4100"))
	if err != nil || port <= 0 {
		return Env{}, fmt.Errorf("PORT must be a positive integer")
	}

	appEnv := get("APP_ENV", EnvDevelopment)
	if appEnv != EnvDevelopment && appEnv != EnvTest && appEnv != EnvProduction {
		return Env{}, fmt.Errorf("APP_ENV must be development, test or production")
	}

	e := Env{
		AppEnv:      appEnv,
		Port:        port,
		CORSOrigins: splitList(get("CORS_ORIGINS", "http://localhost:5173")),
		MongoURI:    get("MONGODB_URI", "mongodb://127.0.0.1:27017"),
		MongoDB:     get("MONGODB_DB", "familytime"),
		AppURL:      strings.TrimRight(get("APP_URL", "http://localhost:5173"), "/"),
		LogLevel:    get("LOG_LEVEL", "info"),
	}
	if e.MongoURI == "" || e.MongoDB == "" {
		return Env{}, fmt.Errorf("MONGODB_URI and MONGODB_DB are required")
	}
	return e, nil
}

func (e Env) IsProduction() bool { return e.AppEnv == EnvProduction }

func get(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func splitList(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
