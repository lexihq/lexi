package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/lexihq/lexi/internal/backend"
)

// fakeBucket is one bucket plus its access keys.
type fakeBucket struct {
	backend.StorageBucket

	keys map[string]backend.BucketKey
}

// bucketNamespace returns the pool's bucket namespace for the request's
// project (buckets route by features.storage.buckets), creating it lazily.
// Callers must hold the mutex.
func (f *Fake) bucketNamespace(ctx context.Context, p *storagePool) map[string]*fakeBucket {
	project := f.featureProject(ctx, "features.storage.buckets")
	if p.buckets == nil {
		p.buckets = map[string]map[string]*fakeBucket{}
	}
	m, ok := p.buckets[project]
	if !ok {
		m = map[string]*fakeBucket{}
		p.buckets[project] = m
	}
	return m
}

func (f *Fake) ListBuckets(ctx context.Context, pool string) ([]backend.StorageBucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	buckets := f.bucketNamespace(ctx, p)
	out := make([]backend.StorageBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, b.StorageBucket)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) CreateBucket(ctx context.Context, pool string, bucket backend.StorageBucket) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if !validAPIName(bucket.Name) {
		return invalid("invalid bucket name %q", bucket.Name)
	}
	buckets := f.bucketNamespace(ctx, p)
	if _, exists := buckets[bucket.Name]; exists {
		return conflict("bucket %q already exists", bucket.Name)
	}
	b := &fakeBucket{
		StorageBucket: backend.StorageBucket{
			Name:        bucket.Name,
			Description: bucket.Description,
			S3URL:       "https://fake-s3:8555/" + bucket.Name,
			Size:        bucket.Size,
		},
		keys: map[string]backend.BucketKey{},
	}
	// Daemon parity: bucket creation seeds an admin credential.
	b.keys["admin"] = f.newBucketKey("admin", "", "admin")
	buckets[bucket.Name] = b
	return nil
}

func (f *Fake) DeleteBucket(ctx context.Context, pool, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	buckets := f.bucketNamespace(ctx, p)
	if _, ok := buckets[name]; !ok {
		return notFoundf("bucket %q", name)
	}
	delete(buckets, name)
	return nil
}

func (f *Fake) ListBucketKeys(ctx context.Context, pool, bucket string) ([]backend.BucketKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	b, err := f.lookupBucket(ctx, pool, bucket)
	if err != nil {
		return nil, err
	}
	out := make([]backend.BucketKey, 0, len(b.keys))
	for _, k := range b.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) CreateBucketKey(ctx context.Context, pool, bucket, name, description string, role backend.BucketKeyRole) (backend.BucketKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	b, err := f.lookupBucket(ctx, pool, bucket)
	if err != nil {
		return backend.BucketKey{}, err
	}
	if !validAPIName(name) {
		return backend.BucketKey{}, invalid("invalid bucket key name %q", name)
	}
	switch role {
	case "":
		role = backend.BucketKeyReadOnly // contract default; the driver applies the same
	case backend.BucketKeyAdmin, backend.BucketKeyReadOnly:
	default:
		return backend.BucketKey{}, invalid("bucket key role %q must be admin or read-only", role)
	}
	if _, exists := b.keys[name]; exists {
		return backend.BucketKey{}, conflict("bucket key %q already exists", name)
	}
	key := f.newBucketKey(name, description, role)
	b.keys[name] = key
	return key, nil
}

func (f *Fake) DeleteBucketKey(ctx context.Context, pool, bucket, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	b, err := f.lookupBucket(ctx, pool, bucket)
	if err != nil {
		return err
	}
	if _, ok := b.keys[name]; !ok {
		return notFoundf("bucket key %q", name)
	}
	delete(b.keys, name)
	return nil
}

// lookupBucket resolves a pool/bucket pair. Callers must hold the mutex.
func (f *Fake) lookupBucket(ctx context.Context, pool, bucket string) (*fakeBucket, error) {
	p, ok := f.remote(ctx).pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	b, ok := f.bucketNamespace(ctx, p)[bucket]
	if !ok {
		return nil, notFoundf("bucket %q", bucket)
	}
	return b, nil
}

// newBucketKey mints a credential with unique fake access/secret keys.
// Callers must hold the mutex.
func (f *Fake) newBucketKey(name, description string, role backend.BucketKeyRole) backend.BucketKey {
	f.bucketKeySeq++
	return backend.BucketKey{
		Name:        name,
		Description: description,
		Role:        role,
		AccessKey:   fmt.Sprintf("FAKEACCESS%06d", f.bucketKeySeq),
		SecretKey:   fmt.Sprintf("fakesecret%06d", f.bucketKeySeq),
	}
}
