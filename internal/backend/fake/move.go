package fake

import (
	"context"
	"fmt"
)

func (f *Fake) RenameInstance(ctx context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if _, ok := sp.instances[newName]; ok {
		return conflict("instance %q already exists", newName)
	}
	in.Name = newName
	delete(sp.instances, name)
	sp.instances[newName] = in
	f.logOp(sp, fmt.Sprintf("Renaming instance %q to %q", name, newName))
	return nil
}

func (f *Fake) MoveInstance(ctx context.Context, name, pool string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	if _, ok := sp.instances[name]; !ok {
		return notFound(name)
	}
	if _, ok := f.remote(ctx).pools[pool]; !ok {
		return notFoundf("storage pool %q", pool)
	}
	// The fake doesn't model per-instance pool placement; a validated no-op.
	f.logOp(sp, fmt.Sprintf("Moving instance %q to pool %q", name, pool))
	return nil
}
