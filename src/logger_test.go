package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitLoggerDevelopment(t *testing.T) {
	// Save and restore global logger
	origLogger := logger
	defer func() { logger = origLogger }()

	err := InitLogger(true, "development")
	assert.NoError(t, err)
	assert.NotNil(t, logger)
}

func TestInitLoggerProduction(t *testing.T) {
	origLogger := logger
	defer func() { logger = origLogger }()

	err := InitLogger(false, "production")
	assert.NoError(t, err)
	assert.NotNil(t, logger)
}

func TestInitLoggerProductionWithDebug(t *testing.T) {
	// When debug=true and env=production, should use development config
	origLogger := logger
	defer func() { logger = origLogger }()

	err := InitLogger(true, "production")
	assert.NoError(t, err)
	assert.NotNil(t, logger)
}

func TestGetLoggerReturnsNonNil(t *testing.T) {
	origLogger := logger
	defer func() { logger = origLogger }()

	// Even when logger is nil, GetLogger returns a nop logger
	logger = nil
	l := GetLogger()
	assert.NotNil(t, l)
}

func TestGetLoggerReturnsInitializedLogger(t *testing.T) {
	origLogger := logger
	defer func() { logger = origLogger }()

	err := InitLogger(false, "development")
	assert.NoError(t, err)

	l := GetLogger()
	assert.NotNil(t, l)
}

func TestSyncLoggerDoesNotPanic(t *testing.T) {
	origLogger := logger
	defer func() { logger = origLogger }()

	// Test with initialized logger
	err := InitLogger(false, "development")
	assert.NoError(t, err)

	assert.NotPanics(t, func() {
		SyncLogger()
	})
}

func TestSyncLoggerWithNilLogger(t *testing.T) {
	origLogger := logger
	defer func() { logger = origLogger }()

	logger = nil
	assert.NotPanics(t, func() {
		SyncLogger()
	})
}
