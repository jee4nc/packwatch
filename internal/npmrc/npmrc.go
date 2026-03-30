// Package npmrc parses .npmrc files to resolve registry URLs and auth tokens.
// It supports scoped registries (@scope:registry=URL) and auth tokens
// (//host/path/:_authToken=TOKEN).
package npmrc

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const defaultRegistry = "https://registry.npmjs.org"

// Config holds parsed .npmrc configuration.
type Config struct {
	DefaultRegistry string            // registry=URL
	ScopeRegistries map[string]string // @scope → registry URL
	AuthTokens      map[string]string // host/path → token
}

// Parse reads .npmrc files (project-level then user-level) and merges them.
// Project-level values take precedence over user-level.
func Parse() Config {
	cfg := Config{
		DefaultRegistry: defaultRegistry,
		ScopeRegistries: make(map[string]string),
		AuthTokens:      make(map[string]string),
	}

	// User-level first (lower priority)
	home, err := os.UserHomeDir()
	if err == nil {
		parseFile(filepath.Join(home, ".npmrc"), &cfg)
	}

	// Project-level second (higher priority, overwrites)
	parseFile(".npmrc", &cfg)

	return cfg
}

// RegistryFor returns the registry URL for a given package name.
func (c Config) RegistryFor(pkgName string) string {
	if strings.HasPrefix(pkgName, "@") {
		scope := pkgName[:strings.Index(pkgName, "/")]
		if reg, ok := c.ScopeRegistries[scope]; ok {
			return strings.TrimSuffix(reg, "/")
		}
	}
	return strings.TrimSuffix(c.DefaultRegistry, "/")
}

// AuthTokenFor returns the auth token for a given registry URL, if any.
func (c Config) AuthTokenFor(registryURL string) string {
	u, err := url.Parse(registryURL)
	if err != nil {
		return ""
	}
	// Try exact host+path match, then progressively shorter paths
	hostPath := u.Host + u.Path
	hostPath = strings.TrimSuffix(hostPath, "/")
	if token, ok := c.AuthTokens[hostPath]; ok {
		return token
	}
	// Try just host
	if token, ok := c.AuthTokens[u.Host]; ok {
		return token
	}
	return ""
}

// IsPrivateRegistry returns true if the registry URL is not the default npm registry.
func (c Config) IsPrivateRegistry(registryURL string) bool {
	return registryURL != defaultRegistry
}

func parseFile(path string, cfg *Config) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Expand environment variables in values
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = expandEnvVars(value)

		switch {
		case key == "registry":
			cfg.DefaultRegistry = value

		case strings.HasPrefix(key, "@") && strings.HasSuffix(key, ":registry"):
			// @scope:registry=URL
			scope := key[:strings.Index(key, ":")]
			cfg.ScopeRegistries[scope] = value

		case strings.HasPrefix(key, "//") && strings.HasSuffix(key, ":_authToken"):
			// //host/path/:_authToken=TOKEN
			hostPath := key[2:]                                    // strip leading //
			hostPath = strings.TrimSuffix(hostPath, ":_authToken") // strip suffix
			hostPath = strings.TrimSuffix(hostPath, "/")           // strip trailing /
			cfg.AuthTokens[hostPath] = value
		}
	}
}

// expandEnvVars replaces ${VAR} and $VAR patterns with environment variable values.
func expandEnvVars(s string) string {
	// Handle ${VAR} syntax
	result := s
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start
		varName := result[start+2 : end]
		envVal := os.Getenv(varName)
		result = result[:start] + envVal + result[end+1:]
	}
	return result
}

// Summary returns a human-readable summary of the config for display.
func (c Config) Summary() string {
	if len(c.ScopeRegistries) == 0 {
		return ""
	}
	var scopes []string
	for scope, reg := range c.ScopeRegistries {
		u, err := url.Parse(reg)
		host := reg
		if err == nil {
			host = u.Host
		}
		hasAuth := c.AuthTokenFor(reg) != ""
		authStr := ""
		if hasAuth {
			authStr = " ✓ auth"
		}
		scopes = append(scopes, fmt.Sprintf("%s → %s%s", scope, host, authStr))
	}
	return strings.Join(scopes, ", ")
}
