package logger

import (
	"fmt"
	"log/slog"

	"github.com/leonkaihao/cache/pkg/model"
)

// DefaultLogger is the default implementation of Logger using slog.
// It uses the default slog logger instance.
type DefaultLogger struct{}

// NewDefaultLogger creates a new DefaultLogger instance
func NewDefaultLogger() model.Logger {
	return &DefaultLogger{}
}

func (l *DefaultLogger) Debug(msg string, keysAndValues ...any) {
	slog.Debug(msg, keysAndValues...)
}

func (l *DefaultLogger) Info(msg string, keysAndValues ...any) {
	slog.Info(msg, keysAndValues...)
}

func (l *DefaultLogger) Error(msg string, keysAndValues ...any) {
	slog.Error(msg, keysAndValues...)
}

func (l *DefaultLogger) Fatal(msg string, keysAndValues ...any) {
	slog.Error(msg, keysAndValues...)
	// Extract error for panic if available
	var err error
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		if key, ok := keysAndValues[i].(string); ok && key == "error" {
			if e, ok := keysAndValues[i+1].(error); ok {
				err = e
				break
			}
		}
	}
	if err != nil {
		panic(err)
	} else {
		panic(fmt.Errorf("%s", msg))
	}
}
