package fake

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

// catalogImages stands in for the full simplestreams catalog the incus driver
// caches. It spans distributions, releases and architectures so handler-level
// filter tests have something to slice. Arches use incus naming.
var catalogImages = []backend.Image{
	{Alias: "debian/12", Fingerprint: "fake-debian-12-aarch64", Description: "Debian 12 (bookworm) arm64", Arch: "aarch64", Distribution: "debian", Release: "12", Variant: "default", Type: "container"},
	{Alias: "debian/12", Fingerprint: "fake-debian-12-x86-64", Description: "Debian 12 (bookworm) amd64", Arch: "x86_64", Distribution: "debian", Release: "12", Variant: "default", Type: "container"},
	{Alias: "ubuntu/24.04", Fingerprint: "fake-ubuntu-24-04-aarch64", Description: "Ubuntu 24.04 LTS arm64", Arch: "aarch64", Distribution: "ubuntu", Release: "24.04", Variant: "default", Type: "container"},
	{Alias: "ubuntu/24.04", Fingerprint: "fake-ubuntu-24-04-vm-x86-64", Description: "Ubuntu 24.04 LTS VM amd64", Arch: "x86_64", Distribution: "ubuntu", Release: "24.04", Variant: "default", Type: "virtual-machine"},
	{Alias: "alpine/edge", Fingerprint: "fake-alpine-edge-aarch64", Description: "Alpine Edge arm64", Arch: "aarch64", Distribution: "alpine", Release: "edge", Variant: "default", Type: "container"},
	{Alias: "fedora/40", Fingerprint: "fake-fedora-40-x86-64", Description: "Fedora 40 amd64", Arch: "x86_64", Distribution: "fedora", Release: "40", Variant: "default", Type: "container"},
}

func (f *Fake) ListImages(_ context.Context) ([]backend.Image, error) {
	return append([]backend.Image(nil), catalogImages...), nil
}

func (f *Fake) ListLocalImages(_ context.Context) ([]backend.LocalImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.LocalImage, 0, len(f.images))
	for _, img := range f.images {
		cp := *img
		cp.Aliases = append([]string(nil), img.Aliases...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out, nil
}

func (f *Fake) PublishImage(_ context.Context, instance, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[instance]; !ok {
		return notFound(instance)
	}
	if alias != "" {
		if owner := f.aliasOwner(alias); owner != nil {
			return conflict("image alias %q", alias)
		}
	}
	created := f.now()
	img := &backend.LocalImage{
		Fingerprint: fmt.Sprintf("pub-%s-%d", instance, created.Unix()),
		Description: fmt.Sprintf("Image from instance %s", instance),
		Arch:        "aarch64",
		Type:        "container",
		CreatedAt:   created,
	}
	if alias != "" {
		img.Aliases = []string{alias}
	}
	f.images[img.Fingerprint] = img
	return nil
}

func (f *Fake) CopyImage(_ context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, c := range catalogImages {
		if c.Alias != alias {
			continue
		}
		if _, ok := f.images[c.Fingerprint]; ok {
			return conflict("image %q", c.Fingerprint)
		}
		f.images[c.Fingerprint] = &backend.LocalImage{
			Fingerprint: c.Fingerprint,
			Aliases:     []string{c.Alias},
			Description: c.Description,
			Arch:        c.Arch,
			SizeBytes:   c.SizeBytes,
			Type:        c.Type,
			CreatedAt:   f.now(),
		}
		return nil
	}
	return notFoundf("image alias %q", alias)
}

func (f *Fake) DeleteImage(_ context.Context, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.images[fingerprint]; !ok {
		return notFoundf("image %q", fingerprint)
	}
	delete(f.images, fingerprint)
	return nil
}

func (f *Fake) AddImageAlias(_ context.Context, fingerprint, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	img, ok := f.images[fingerprint]
	if !ok {
		return notFoundf("image %q", fingerprint)
	}
	if owner := f.aliasOwner(alias); owner != nil {
		return conflict("image alias %q", alias)
	}
	img.Aliases = append(img.Aliases, alias)
	return nil
}

func (f *Fake) RemoveImageAlias(_ context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	img := f.aliasOwner(alias)
	if img == nil {
		return notFoundf("image alias %q", alias)
	}
	img.Aliases = slices.DeleteFunc(img.Aliases, func(a string) bool { return a == alias })
	return nil
}

// aliasOwner returns the local image carrying alias, or nil. Callers must hold
// the mutex.
func (f *Fake) aliasOwner(alias string) *backend.LocalImage {
	for _, img := range f.images {
		if slices.Contains(img.Aliases, alias) {
			return img
		}
	}
	return nil
}
