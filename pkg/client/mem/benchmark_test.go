package mem

import (
	"os"
	"testing"

	"github.com/leonkaihao/cache/v2/pkg/logger"
)

// TestMain sets up the test environment for all tests and benchmarks in this package.
// It sets a silent logger to avoid cluttering benchmark output with log messages.
func TestMain(m *testing.M) {
	// Set silent logger for ALL benchmarks/tests in this package
	SetLogger(logger.NewSilentLogger())

	// Run tests
	os.Exit(m.Run())
}
