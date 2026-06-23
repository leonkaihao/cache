//go:build integration

package redis

import (
	"testing"

	"github.com/leonkaihao/cache/pkg/client/test"
	"github.com/leonkaihao/cache/pkg/coding"
	"github.com/leonkaihao/cache/pkg/model"
)

func TestRedisSuite(t *testing.T) {
	suite := test.TestSuite{
		Name: "Redis",
		BucketFactory: func() (model.CacheBucket, error) {
			cli := NewClient(getRedisAddr(), "admin", 1)
			return NewBucket[test.TestData](cli, "test_suite", coding.NewJsonCoder())
		},
		ClientFactory: func() model.CacheClient {
			return NewClient(getRedisAddr(), "admin", 1)
		},
	}
	suite.RunAll(t)
}
