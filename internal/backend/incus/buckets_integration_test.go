//go:build integration

package incus

import (
	"context"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStorageBucketRoundTrip creates a bucket on the default pool,
// round-trips an access key, and deletes everything. No size quota: the
// dir driver rejects bucket sizes ("Size cannot be specified for buckets"),
// so quotas are exercised at the stub/fake/UI layers only. Local-pool
// buckets need core.storage_buckets_address (and MinIO) on the host;
// daemons without that setup skip rather than fail.
func TestStorageBucketRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).StorageBuckets {
		t.Skip("daemon lacks the storage_buckets extension")
	}
	ctx := context.Background()
	name := uniqueName("lxbucket")
	t.Cleanup(func() { _ = b.DeleteBucket(ctx, "default", name) })

	if err := b.CreateBucket(ctx, "default", name, "made by test", ""); err != nil {
		reason := strings.ToLower(err.Error())
		if strings.Contains(reason, "storage_buckets_address") || strings.Contains(reason, "minio") {
			t.Skipf("host not set up for local buckets: %v", err)
		}
		t.Fatalf("create bucket: %v", err)
	}
	require.ErrorIs(t, b.CreateBucket(ctx, "default", name, "", ""), backend.ErrConflict)

	buckets, err := b.ListBuckets(ctx, "default")
	require.NoError(t, err)
	var found *backend.StorageBucket
	for i := range buckets {
		if buckets[i].Name == name {
			found = &buckets[i]
		}
	}
	require.NotNil(t, found, "created bucket not listed")
	assert.Equal(t, "made by test", found.Description)
	assert.NotEmpty(t, found.S3URL)

	// The daemon seeds an admin key; a created key returns generated creds.
	keys, err := b.ListBucketKeys(ctx, "default", name)
	require.NoError(t, err)
	require.NotEmpty(t, keys)

	key, err := b.CreateBucketKey(ctx, "default", name, "lexi-ci", "ci reader", "read-only")
	require.NoError(t, err)
	assert.Equal(t, "read-only", key.Role)
	assert.NotEmpty(t, key.AccessKey)
	assert.NotEmpty(t, key.SecretKey)
	_, err = b.CreateBucketKey(ctx, "default", name, "lexi-ci", "", "")
	require.ErrorIs(t, err, backend.ErrConflict)
	require.NoError(t, b.DeleteBucketKey(ctx, "default", name, "lexi-ci"))

	require.NoError(t, b.DeleteBucket(ctx, "default", name))
	require.ErrorIs(t, b.DeleteBucket(ctx, "default", name), backend.ErrNotFound)
}
