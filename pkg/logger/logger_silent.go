package logger

import "github.com/leonkaihao/cache/v2/pkg/model"

func NewSilentLogger() model.Logger {
	return &SilentLogger{}
}

type SilentLogger struct{}

func (l *SilentLogger) Debug(msg string, keysAndValues ...any) {}

func (l *SilentLogger) Info(msg string, keysAndValues ...any) {}

func (l *SilentLogger) Error(msg string, keysAndValues ...any) {}

func (l *SilentLogger) Fatal(msg string, keysAndValues ...any) {}
