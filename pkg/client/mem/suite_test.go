package mem

import (
	"testing"

	"github.com/leonkaihao/cache/v2/pkg/client/test"
	"github.com/leonkaihao/cache/v2/pkg/model"
)

func TestMemorySuite(t *testing.T) {
	suite := test.TestSuite{
		Name: "Memory",
		BucketFactory: func() (model.CacheBucket, error) {
			cli := NewClient()
			return NewBucket[test.TestData](cli, "test_suite")
		},
		ClientFactory: func() model.CacheClient {
			return NewClient()
		},
	}
	suite.RunAll(t)
}
