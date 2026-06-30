package logger

import (
	"fmt"

	"github.com/leonkaihao/cache/v2/pkg/model"
	"go.uber.org/zap"
)

// DefaultLogger is the default implementation of Logger using zap.
type DefaultLogger struct {
	sugar *zap.SugaredLogger
}

// NewDefaultLogger creates a new DefaultLogger instance
func NewDefaultLogger() model.Logger {
	logger, _ := zap.NewProduction()
	return &DefaultLogger{sugar: logger.Sugar()}
}

func (l *DefaultLogger) Debug(msg string, keysAndValues ...any) {
	l.sugar.Debugw(msg, keysAndValues...)
}

func (l *DefaultLogger) Info(msg string, keysAndValues ...any) {
	l.sugar.Infow(msg, keysAndValues...)
}

func (l *DefaultLogger) Error(msg string, keysAndValues ...any) {
	l.sugar.Errorw(msg, keysAndValues...)
}

func (l *DefaultLogger) Fatal(msg string, keysAndValues ...any) {
	l.sugar.Errorw(msg, keysAndValues...)
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
