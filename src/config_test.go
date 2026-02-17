package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetEnvWithDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		want         string
	}{
		{
			name:         "returns env value when set",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "from_env",
			want:         "from_env",
		},
		{
			name:         "returns default when env not set",
			key:          "MISSING_KEY",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvWithDefault(tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvBoolWithDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		want         bool
	}{
		{
			name:         "returns true from env",
			key:          "TEST_BOOL",
			defaultValue: false,
			envValue:     "true",
			want:         true,
		},
		{
			name:         "returns false from env",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "false",
			want:         false,
		},
		{
			name:         "returns default when env not set",
			key:          "MISSING_BOOL",
			defaultValue: true,
			envValue:     "",
			want:         true,
		},
		{
			name:         "returns default on invalid value",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "invalid",
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := getEnvBoolWithDefault(tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvInt64WithDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int64
		envValue     string
		want         int64
	}{
		{
			name:         "returns int64 from env",
			key:          "TEST_INT",
			defaultValue: 100,
			envValue:     "200",
			want:         200,
		},
		{
			name:         "returns default when env not set",
			key:          "MISSING_INT",
			defaultValue: 100,
			envValue:     "",
			want:         100,
		},
		{
			name:         "returns default on invalid value",
			key:          "TEST_INT",
			defaultValue: 100,
			envValue:     "not_a_number",
			want:         100,
		},
		{
			name:         "handles large numbers",
			key:          "TEST_INT",
			defaultValue: 0,
			envValue:     "209715200",
			want:         209715200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := getEnvInt64WithDefault(tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetClamdClient(t *testing.T) {
	// Save original config
	originalSocket := config.ClamdUnixSocket

	tests := []struct {
		name       string
		socketPath string
	}{
		{
			name:       "valid socket path",
			socketPath: "/var/run/clamav/clamd.ctl",
		},
		{
			name:       "invalid socket path",
			socketPath: "/nonexistent/path/clamd.ctl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.ClamdUnixSocket = tt.socketPath
			resetClamdClient()
			defer func() {
				config.ClamdUnixSocket = originalSocket
				resetClamdClient()
			}()

			client := getClamdClient()
			assert.NotNil(t, client, "getClamdClient should always return a non-nil client")
		})
	}
}

func TestPingClamd(t *testing.T) {
	// Save original config
	originalSocket := config.ClamdUnixSocket

	t.Run("ping with invalid socket returns error", func(t *testing.T) {
		config.ClamdUnixSocket = "/nonexistent/socket.ctl"
		resetClamdClient()
		defer func() {
			config.ClamdUnixSocket = originalSocket
			resetClamdClient()
		}()

		err := pingClamd()
		assert.Error(t, err)
		t.Logf("Expected error: %v", err)
	})

	t.Run("ping with default socket", func(t *testing.T) {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
		defer func() {
			config.ClamdUnixSocket = originalSocket
			resetClamdClient()
		}()

		err := pingClamd()
		if err != nil {
			t.Logf("Ping failed (ClamAV may not be running): %v", err)
		} else {
			t.Log("Ping succeeded - ClamAV is running")
		}
	})
}

func TestConfigDefaults(t *testing.T) {
	// Test that default config values are sane
	// Note: config may be modified by other tests or init functions
	assert.NotEmpty(t, config.ClamdUnixSocket)
	assert.True(t, strings.Contains(config.ClamdUnixSocket, "clamd.ctl") || strings.Contains(config.ClamdUnixSocket, "clamd.sock"),
		"socket path should contain clamd.ctl or clamd.sock, got: %s", config.ClamdUnixSocket)
	assert.Equal(t, int64(209715200), config.MaxContentLength) // 200MB
	assert.Equal(t, "0.0.0.0", config.Host)
	assert.Equal(t, "6000", config.Port)
	assert.Equal(t, "9000", config.GRPCPort)
	assert.Equal(t, 300*time.Second, config.ScanTimeout)
	assert.Equal(t, true, config.EnableGRPC)
}

func TestParseConfigEnvOverrides(t *testing.T) {
	// Save and restore global config + flag state
	origConfig := config
	defer func() {
		config = origConfig
		resetClamdClient()
	}()

	// Reset flag.CommandLine so parseConfig can re-register flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	// Set env vars to override defaults
	envVars := map[string]string{
		"CLAMAV_DEBUG":        "true",
		"CLAMAV_SOCKET":       "/custom/clamd.sock",
		"CLAMAV_MAX_SIZE":     "1048576",
		"CLAMAV_HOST":         "127.0.0.1",
		"CLAMAV_PORT":         "7000",
		"CLAMAV_GRPC_PORT":    "9500",
		"CLAMAV_ENABLE_GRPC":  "false",
		"CLAMAV_SCAN_TIMEOUT": "60",
	}
	for k, v := range envVars {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range envVars {
			os.Unsetenv(k)
		}
	}()

	parseConfig()

	assert.True(t, config.Debug)
	assert.Equal(t, "/custom/clamd.sock", config.ClamdUnixSocket)
	assert.Equal(t, int64(1048576), config.MaxContentLength)
	assert.Equal(t, "127.0.0.1", config.Host)
	assert.Equal(t, "7000", config.Port)
	assert.Equal(t, "9500", config.GRPCPort)
	assert.False(t, config.EnableGRPC)
	assert.Equal(t, 60*time.Second, config.ScanTimeout)
}

func TestParseConfigGinModes(t *testing.T) {
	origConfig := config
	defer func() {
		config = origConfig
		resetClamdClient()
	}()

	tests := []struct {
		name     string
		ginMode  string
		envMode  string
		debug    string
		expected string
	}{
		{
			name:     "GIN_MODE env takes priority",
			ginMode:  "release",
			envMode:  "",
			debug:    "true",
			expected: "release",
		},
		{
			name:     "production env without debug sets release",
			ginMode:  "",
			envMode:  "production",
			debug:    "false",
			expected: "release",
		},
		{
			name:     "default is debug mode",
			ginMode:  "",
			envMode:  "",
			debug:    "false",
			expected: "debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global config to defaults before each subtest
			config = origConfig
			resetClamdClient()

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)

			// Clear all relevant env vars first
			os.Unsetenv("GIN_MODE")
			os.Unsetenv("ENV")
			os.Unsetenv("CLAMAV_DEBUG")

			// Set minimal valid env
			os.Setenv("CLAMAV_SOCKET", "/tmp/test.sock")
			os.Setenv("CLAMAV_PORT", "6000")
			os.Setenv("CLAMAV_GRPC_PORT", "9000")
			os.Setenv("CLAMAV_DEBUG", tt.debug)
			defer func() {
				os.Unsetenv("CLAMAV_SOCKET")
				os.Unsetenv("CLAMAV_PORT")
				os.Unsetenv("CLAMAV_GRPC_PORT")
				os.Unsetenv("GIN_MODE")
				os.Unsetenv("ENV")
				os.Unsetenv("CLAMAV_DEBUG")
			}()

			if tt.ginMode != "" {
				os.Setenv("GIN_MODE", tt.ginMode)
			}
			if tt.envMode != "" {
				os.Setenv("ENV", tt.envMode)
			}

			parseConfig()

			assert.Equal(t, tt.expected, gin.Mode())
		})
	}
}

func TestParseConfigValidationExits(t *testing.T) {
	// Each subtest uses os/exec to run itself in a subprocess so os.Exit(1) doesn't kill the test runner.
	tests := []struct {
		name       string
		envKey     string
		envValue   string
		wantStderr string
	}{
		{
			name:       "negative scan timeout exits",
			envKey:     "CLAMAV_SCAN_TIMEOUT",
			envValue:   "-1",
			wantStderr: "FATAL: scan timeout must be > 0",
		},
		{
			name:       "negative max content length exits",
			envKey:     "CLAMAV_MAX_SIZE",
			envValue:   "-1",
			wantStderr: "FATAL: max content length must be > 0",
		},
		{
			name:       "empty socket path exits",
			envKey:     "CLAMAV_SOCKET",
			envValue:   "",
			wantStderr: "FATAL: ClamAV Unix socket path must not be empty",
		},
		{
			name:       "invalid REST port exits",
			envKey:     "CLAMAV_PORT",
			envValue:   "not_a_port",
			wantStderr: "FATAL: port must be a valid TCP port",
		},
		{
			name:       "invalid gRPC port exits",
			envKey:     "CLAMAV_GRPC_PORT",
			envValue:   "99999",
			wantStderr: "FATAL: gRPC port must be a valid TCP port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// If we're in the subprocess, run parseConfig and let it exit
			if os.Getenv("TEST_SUBPROCESS") == "1" {
				flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
				// Set valid defaults first, then override the one we're testing
				os.Setenv("CLAMAV_SOCKET", "/tmp/test.sock")
				os.Setenv("CLAMAV_PORT", "6000")
				os.Setenv("CLAMAV_GRPC_PORT", "9000")
				os.Setenv("CLAMAV_SCAN_TIMEOUT", "60")
				os.Setenv("CLAMAV_MAX_SIZE", "1024")

				// Apply the test-specific override
				if tt.envValue == "" {
					os.Setenv(tt.envKey, "")
				} else {
					os.Setenv(tt.envKey, tt.envValue)
				}

				parseConfig()
				return // should not reach here
			}

			// Parent process: run self as subprocess
			cmd := exec.Command(os.Args[0], "-test.run=^TestParseConfigValidationExits$/^"+tt.name+"$")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=1")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			err := cmd.Run()
			assert.Error(t, err, "expected parseConfig to call os.Exit(1)")

			exitErr, ok := err.(*exec.ExitError)
			if ok {
				assert.Equal(t, 1, exitErr.ExitCode())
			}

			assert.Contains(t, stderr.String(), tt.wantStderr,
				"stderr should contain expected fatal message")
		})
	}
}
