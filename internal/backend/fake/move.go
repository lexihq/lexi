package fake

import "context"

func (f *Fake) RenameInstance(_ context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	if _, ok := f.instances[newName]; ok {
		return conflict("instance %q already exists", newName)
	}
	in.Name = newName
	delete(f.instances, name)
	f.instances[newName] = in
	return nil
}

func (f *Fake) MoveInstance(_ context.Context, name, pool string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return notFound(name)
	}
	if _, ok := f.pools[pool]; !ok {
		return notFoundf("storage pool %q", pool)
	}
	// The fake doesn't model per-instance pool placement; a validated no-op.
	return nil
}
