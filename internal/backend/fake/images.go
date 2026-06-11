package fake

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

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
		Description: "Image from instance " + instance,
		Arch:        "aarch64",
		Type:        "container",
		CreatedAt:   created,
	}
	if alias != "" {
		img.Aliases = []string{alias}
	}
	f.images[img.Fingerprint] = img
	f.logOp(fmt.Sprintf("Publishing image from instance %q", instance))
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
		f.logOp(fmt.Sprintf("Copying image %q", alias))
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
	f.logOp(fmt.Sprintf("Deleting image %q", fingerprint))
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

// fakeImageMagic prefixes the deterministic blob ExportImage writes so
// ImportImage can recognize a lxcon-produced image tarball, mirroring the
// instance backup round-trip.
const fakeImageMagic = "lxcon-fake-image\n"

// fakeRootfsMagic prefixes the rootfs entry of a fake split-image zip.
const fakeRootfsMagic = "lxcon-fake-rootfs\n"

// SeedSplitImage adds a VM-type local image for tests and the e2e fakeserver
// (PublishImage only makes container images). VM images export as split zips,
// like the daemon stores them.
func (f *Fake) SeedSplitImage(fingerprint, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.images[fingerprint] = &backend.LocalImage{
		Fingerprint: fingerprint,
		Description: description,
		Arch:        "aarch64",
		Type:        "virtual-machine",
		CreatedAt:   f.now(),
	}
}

// ExportImage returns a deterministic blob carrying the fingerprint so the
// export→import round-trip is observable: a plain magic blob for container
// images, a metadata+rootfs.img zip for VM (split) images.
func (f *Fake) ExportImage(_ context.Context, fingerprint string) (backend.ImageExportFormat, io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	img, ok := f.images[fingerprint]
	if !ok {
		return "", nil, notFoundf("image %q", fingerprint)
	}
	if img.Type != "virtual-machine" {
		return backend.ImageExportUnified, io.NopCloser(strings.NewReader(fakeImageMagic + fingerprint)), nil
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, payload string }{
		{"metadata", fakeImageMagic + fingerprint},
		{"rootfs.img", fakeRootfsMagic + fingerprint},
	} {
		// Store, not Deflate: the real payloads are already compressed, and
		// ImportImage rejects compressed entries as a zip-bomb guard.
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		if err != nil {
			return "", nil, err
		}
		if _, err := io.WriteString(w, e.payload); err != nil {
			return "", nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}
	return backend.ImageExportSplitZip, io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// ImportImage recreates an image from a blob ExportImage wrote — unified
// magic blob or split zip — rejecting foreign data with ErrInvalid and
// prefixing the recovered fingerprint so the original can coexist.
func (f *Fake) ImportImage(_ context.Context, r io.Reader, alias string) error {
	blob, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	orig, imgType, err := parseFakeImageBlob(blob)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	fingerprint := "imported-" + orig
	if _, exists := f.images[fingerprint]; exists {
		return conflict("image %q already exists", fingerprint)
	}
	if alias != "" {
		if owner := f.aliasOwner(alias); owner != nil {
			return conflict("image alias %q", alias)
		}
	}
	img := &backend.LocalImage{
		Fingerprint: fingerprint,
		Description: "Imported image",
		Arch:        "aarch64",
		Type:        imgType,
		CreatedAt:   f.now(),
	}
	if alias != "" {
		img.Aliases = []string{alias}
	}
	f.images[fingerprint] = img
	f.logOp(fmt.Sprintf("Importing image %q", fingerprint))
	return nil
}

// parseFakeImageBlob recognizes the two export shapes and recovers the
// original fingerprint plus the image type. Split zips must carry exactly a
// "metadata" entry and a "rootfs"/"rootfs.img" entry (the name encodes the
// type), both stored uncompressed — the same contract the incus driver
// enforces.
func parseFakeImageBlob(blob []byte) (fingerprint, imgType string, err error) {
	if orig, ok := strings.CutPrefix(string(blob), fakeImageMagic); ok {
		return orig, "container", nil
	}
	if !bytes.HasPrefix(blob, []byte("PK\x03\x04")) {
		return "", "", fmt.Errorf("not a lxcon image tarball: %w", backend.ErrInvalid)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return "", "", fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
	}
	var meta []byte
	imgType = ""
	for _, zf := range zr.File {
		if zf.Method != zip.Store {
			return "", "", fmt.Errorf("split-image zip entry %q is compressed: %w", zf.Name, backend.ErrInvalid)
		}
		switch zf.Name {
		case "metadata":
			rc, err := zf.Open()
			if err != nil {
				return "", "", fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
			}
			meta, err = io.ReadAll(rc)
			closeErr := rc.Close()
			if err != nil || closeErr != nil {
				return "", "", fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
			}
		case "rootfs":
			imgType = "container"
		case "rootfs.img":
			imgType = "virtual-machine"
		default:
			return "", "", fmt.Errorf("unexpected split-image zip entry %q: %w", zf.Name, backend.ErrInvalid)
		}
	}
	orig, ok := strings.CutPrefix(string(meta), fakeImageMagic)
	if !ok || imgType == "" {
		return "", "", fmt.Errorf("split-image zip is missing metadata or rootfs: %w", backend.ErrInvalid)
	}
	return orig, imgType, nil
}

// UpdateImage sets the image's description and public flag.
func (f *Fake) UpdateImage(_ context.Context, fingerprint, description string, public bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	img, ok := f.images[fingerprint]
	if !ok {
		return notFoundf("image %q", fingerprint)
	}
	img.Description = description
	img.Public = public
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
