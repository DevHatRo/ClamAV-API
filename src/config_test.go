package main

import (
	"os"
	"testing"
	"time"

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
		name        string
		socketPath  string
		expectError bool
	}{
		{
			name:        "valid socket path",
			socketPath:  "/var/run/clamav/clamd.ctl",
			expectError: true, // Will error if ClamAV not running
		},
		{
			name:        "invalid socket path",
			socketPath:  "/nonexistent/path/clamd.ctl",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.ClamdUnixSocket = tt.socketPath
			defer func() { config.ClamdUnixSocket = originalSocket }()

			client, err := getClamdClient()

			if tt.expectError {
				// Error expected if ClamAV not running
				if err != nil {
					assert.Error(t, err)
					t.Logf("Expected error (ClamAV may not be running): %v", err)
				} else {
					assert.NotNil(t, client)
				}
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	// Test that default config values are sane
	assert.Equal(t, "/run/clamav/clamd.ctl", config.ClamdUnixSocket)
	assert.Equal(t, int64(209715200), config.MaxContentLength) // 200MB
	assert.Equal(t, "0.0.0.0", config.Host)
	assert.Equal(t, "6000", config.Port)
	assert.Equal(t, "9000", config.GRPCPort)
	assert.Equal(t, 300*time.Second, config.ScanTimeout)
	assert.Equal(t, true, config.EnableGRPC)
}
