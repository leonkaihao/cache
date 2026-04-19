package mem

import (
	"github.com/leonkaihao/cache/pkg/logger"
	"github.com/leonkaihao/cache/pkg/model"
)

// Logger is the package-level logger for mem cache operations.
// By default, it uses the DefaultLogger which wraps slog.
// You can replace it with your own implementation using SetLogger.
var Logger model.Logger = logger.NewDefaultLogger()

// SetLogger sets the package-level logger for all mem cache operations.
// This should be called once at application startup before creating any cache clients.
func SetLogger(logger model.Logger) {
	Logger = logger
}
