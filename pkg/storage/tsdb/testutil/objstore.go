package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/pao214/loki/pkg/storage/bucket/filesystem"
)

func PrepareFilesystemBucket(t testing.TB) (objstore.Bucket, string) {
	storageDir := t.TempDir()

	bkt, err := filesystem.NewBucketClient(filesystem.Config{Directory: storageDir})
	require.NoError(t, err)

	return objstore.BucketWithMetrics("test", bkt, nil), storageDir
}
