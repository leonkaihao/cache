//go:build integration

package redis

import "os"

// getRedisAddr returns the Redis address from environment variable or default
func getRedisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}
