package model

// Logger is an interface for logging in the cache system.
// Users can provide their own implementation to integrate with their logging framework.
type Logger interface {
	// Debug logs a debug message with optional key-value pairs
	Debug(msg string, keysAndValues ...any)
	// Info logs an informational message with optional key-value pairs
	Info(msg string, keysAndValues ...any)
	// Error logs an error message with optional key-value pairs
	Error(msg string, keysAndValues ...any)
	// Fatal logs an error message and then panics
	// This is used for critical errors that should stop execution
	Fatal(msg string, keysAndValues ...any)
}
