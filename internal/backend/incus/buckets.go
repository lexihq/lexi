package incus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListBuckets(ctx context.Context, pool string) ([]backend.StorageBucket, error) {
	buckets, err := b.project(ctx).GetStoragePoolBuckets(pool)
	if err != nil {
		return nil, fmt.Errorf("list buckets of pool %q: %w", pool, mapErr(err))
	}
	out := make([]backend.StorageBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, backend.StorageBucket{
			Name:        bucket.Name,
			Description: bucket.Description,
			S3URL:       bucket.S3URL,
			Size:        bucket.Config["size"],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateBucket creates a bucket; the daemon also mints an initial admin key,
// which callers read back via ListBucketKeys rather than from here.
func (b *incusBackend) CreateBucket(ctx context.Context, pool string, bucket backend.StorageBucket) error {
	post := api.StorageBucketsPost{Name: bucket.Name}
	post.Description = bucket.Description
	if bucket.Size != "" {
		post.Config = api.ConfigMap{"size": bucket.Size}
	}
	if _, err := b.project(ctx).CreateStoragePoolBucket(pool, post); err != nil {
		// The daemon reports a duplicate as a plain 400, which mapErr's typed
		// BadRequest branch would turn into ErrInvalid before the string
		// fallback can see it.
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("bucket %q already exists: %w", bucket.Name, backend.ErrConflict)
		}
		return fmt.Errorf("create bucket %q in pool %q: %w", bucket.Name, pool, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteBucket(ctx context.Context, pool, name string) error {
	if err := b.project(ctx).DeleteStoragePoolBucket(pool, name); err != nil {
		return fmt.Errorf("delete bucket %q from pool %q: %w", name, pool, mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListBucketKeys(ctx context.Context, pool, bucket string) ([]backend.BucketKey, error) {
	keys, err := b.project(ctx).GetStoragePoolBucketKeys(pool, bucket)
	if err != nil {
		return nil, fmt.Errorf("list keys of bucket %q: %w", bucket, mapErr(err))
	}
	out := make([]backend.BucketKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, toBucketKey(&k))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateBucketKey adds a credential; the daemon generates the access/secret
// pair, returned to the caller.
func (b *incusBackend) CreateBucketKey(ctx context.Context, pool, bucket, name, description, role string) (backend.BucketKey, error) {
	// The contract defaults an empty role to read-only (the handler and the fake
	// both permit ""); the daemon rejects an empty role rather than defaulting it,
	// so apply the default here.
	if role == "" {
		role = "read-only"
	}
	post := api.StorageBucketKeysPost{}
	post.Name = name
	post.Description = description
	post.Role = role
	created, err := b.project(ctx).CreateStoragePoolBucketKey(pool, bucket, post)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return backend.BucketKey{}, fmt.Errorf("bucket key %q already exists: %w", name, backend.ErrConflict)
		}
		return backend.BucketKey{}, fmt.Errorf("create key %q for bucket %q: %w", name, bucket, mapErr(err))
	}
	return toBucketKey(created), nil
}

func (b *incusBackend) DeleteBucketKey(ctx context.Context, pool, bucket, name string) error {
	if err := b.project(ctx).DeleteStoragePoolBucketKey(pool, bucket, name); err != nil {
		return fmt.Errorf("delete key %q from bucket %q: %w", name, bucket, mapErr(err))
	}
	return nil
}

func toBucketKey(k *api.StorageBucketKey) backend.BucketKey {
	return backend.BucketKey{
		Name:        k.Name,
		Description: k.Description,
		Role:        k.Role,
		AccessKey:   k.AccessKey,
		SecretKey:   k.SecretKey,
	}
}
