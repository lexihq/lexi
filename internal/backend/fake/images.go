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

	"github.com/lexihq/lexi/internal/backend"
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

func (f *Fake) ListImages(ctx context.Context) ([]backend.Image, error) {
	return append([]backend.Image(nil), catalogImages...), nil
}

func (f *Fake) ListLocalImages(ctx context.Context) ([]backend.LocalImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	out := make([]backend.LocalImage, 0, len(sp.images))
	for _, img := range sp.images {
		cp := *img
		cp.Aliases = append([]string(nil), img.Aliases...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out, nil
}

func (f *Fake) PublishImage(ctx context.Context, instance, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The source instance lives in the request's project; the image lands in
	// the image-owning space (default when features.images is off).
	sp := f.featureSpace(ctx, "features.images")
	if _, ok := f.space(ctx).instances[instance]; !ok {
		return notFound(instance)
	}
	if alias != "" {
		if owner := aliasOwner(sp, alias); owner != nil {
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
	sp.images[img.Fingerprint] = img
	f.logOp(sp, fmt.Sprintf("Publishing image from instance %q", instance))
	return nil
}

func (f *Fake) CopyImage(ctx context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	for _, c := range catalogImages {
		if c.Alias != alias {
			continue
		}
		if _, ok := sp.images[c.Fingerprint]; ok {
			return conflict("image %q", c.Fingerprint)
		}
		sp.images[c.Fingerprint] = &backend.LocalImage{
			Fingerprint:     c.Fingerprint,
			Aliases:         []string{c.Alias},
			Description:     c.Description,
			Arch:            c.Arch,
			SizeBytes:       c.SizeBytes,
			Type:            c.Type,
			HasUpdateSource: true,
			CreatedAt:       f.now(),
		}
		f.logOp(sp, fmt.Sprintf("Copying image %q", alias))
		return nil
	}
	return notFoundf("image alias %q", alias)
}

func (f *Fake) DeleteImage(ctx context.Context, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	if _, ok := sp.images[fingerprint]; !ok {
		return notFoundf("image %q", fingerprint)
	}
	delete(sp.images, fingerprint)
	f.logOp(sp, fmt.Sprintf("Deleting image %q", fingerprint))
	return nil
}

func (f *Fake) AddImageAlias(ctx context.Context, fingerprint, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	img, ok := sp.images[fingerprint]
	if !ok {
		return notFoundf("image %q", fingerprint)
	}
	if owner := aliasOwner(sp, alias); owner != nil {
		return conflict("image alias %q", alias)
	}
	img.Aliases = append(img.Aliases, alias)
	return nil
}

func (f *Fake) RemoveImageAlias(ctx context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	img := aliasOwner(sp, alias)
	if img == nil {
		return notFoundf("image alias %q", alias)
	}
	img.Aliases = slices.DeleteFunc(img.Aliases, func(a string) bool { return a == alias })
	return nil
}

// fakeImageMagic prefixes the deterministic blob ExportImage writes so
// ImportImage can recognize a lexi-produced image tarball, mirroring the
// instance backup round-trip.
const fakeImageMagic = "lexi-fake-image\n"

// fakeRootfsMagic prefixes the rootfs entry of a fake split-image zip.
const fakeRootfsMagic = "lexi-fake-rootfs\n"

// SeedSplitImage adds a VM-type local image for tests and the e2e fakeserver
// (PublishImage only makes container images). VM images export as split zips,
// like the daemon stores them.
func (f *Fake) SeedSplitImage(fingerprint, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.remoteFor("local").spaceFor("default")

	sp.images[fingerprint] = &backend.LocalImage{
		Fingerprint: fingerprint,
		Description: description,
		Arch:        "aarch64",
		Type:        "virtual-machine",
		CreatedAt:   f.now(),
	}
}

// ExportImage returns a deterministic blob carrying the fingerprint so the
// export→import round-trip is observable: a plain magic blob for container
// images, a metadata+rootfs.img zip for VM (split) images. The filename
// mirrors the incus driver's naming (daemon-suggested name vs fingerprint
// zip).
func (f *Fake) ExportImage(ctx context.Context, fingerprint string) (string, io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	img, ok := sp.images[fingerprint]
	if !ok {
		return "", nil, notFoundf("image %q", fingerprint)
	}
	if img.Type != backend.TypeVirtualMachine {
		return fingerprint + ".tar", io.NopCloser(strings.NewReader(fakeImageMagic + fingerprint)), nil
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
	return fingerprint + ".zip", io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// ImportImage recreates an image from a blob ExportImage wrote — unified
// magic blob or split zip — rejecting foreign data with ErrInvalid and
// prefixing the recovered fingerprint so the original can coexist.
func (f *Fake) ImportImage(ctx context.Context, r io.Reader, alias string) error {
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
	sp := f.featureSpace(ctx, "features.images")

	fingerprint := "imported-" + orig
	if _, exists := sp.images[fingerprint]; exists {
		return conflict("image %q already exists", fingerprint)
	}
	if alias != "" {
		if owner := aliasOwner(sp, alias); owner != nil {
			return conflict("image alias %q", alias)
		}
	}
	img := &backend.LocalImage{
		Fingerprint: fingerprint,
		Description: "Imported image",
		Arch:        "aarch64",
		Type:        backend.InstanceType(imgType),
		CreatedAt:   f.now(),
	}
	if alias != "" {
		img.Aliases = []string{alias}
	}
	sp.images[fingerprint] = img
	f.logOp(sp, fmt.Sprintf("Importing image %q", fingerprint))
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
		return "", "", fmt.Errorf("not a lexi image tarball: %w", backend.ErrInvalid)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return "", "", fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
	}
	var meta, rootfs []byte
	imgType = ""
	readEntry := func(zf *zip.File) ([]byte, error) {
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
		}
		data, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil || closeErr != nil {
			return nil, fmt.Errorf("corrupt split-image zip: %w", backend.ErrInvalid)
		}
		return data, nil
	}
	for _, zf := range zr.File {
		if zf.Method != zip.Store {
			return "", "", fmt.Errorf("split-image zip entry %q is compressed: %w", zf.Name, backend.ErrInvalid)
		}
		switch zf.Name {
		case "metadata":
			if meta, err = readEntry(zf); err != nil {
				return "", "", err
			}
		case "rootfs", "rootfs.img":
			// The real driver streams the rootfs to the daemon; the fake
			// reads it so corrupt entries fail here too.
			if rootfs, err = readEntry(zf); err != nil {
				return "", "", err
			}
			imgType = "container"
			if zf.Name == "rootfs.img" {
				imgType = "virtual-machine"
			}
		default:
			return "", "", fmt.Errorf("unexpected split-image zip entry %q: %w", zf.Name, backend.ErrInvalid)
		}
	}
	orig, ok := strings.CutPrefix(string(meta), fakeImageMagic)
	if !ok || imgType == "" || !strings.HasPrefix(string(rootfs), fakeRootfsMagic) {
		return "", "", fmt.Errorf("split-image zip is missing metadata or rootfs: %w", backend.ErrInvalid)
	}
	return orig, imgType, nil
}

// UpdateImage sets the image's description and public flag.
func (f *Fake) UpdateImage(ctx context.Context, fingerprint string, edit backend.ImageEdit) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	img, ok := sp.images[fingerprint]
	if !ok {
		return notFoundf("image %q", fingerprint)
	}
	img.Description = edit.Description
	img.Public = edit.Public
	img.AutoUpdate = edit.AutoUpdate
	img.ExpiresAt = edit.ExpiresAt
	return nil
}

// RefreshImage re-pulls an image from its update source; the fake just logs
// the operation. Locally published or imported images have no source.
func (f *Fake) RefreshImage(ctx context.Context, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.images")

	img, ok := sp.images[fingerprint]
	if !ok {
		return notFoundf("image %q", fingerprint)
	}
	if !img.HasUpdateSource {
		return invalid("image %q has no update source to refresh from", fingerprint)
	}
	f.logOp(f.space(ctx), fmt.Sprintf("Refreshing image %q", fingerprint))
	return nil
}

// aliasOwner returns the local image carrying alias, or nil. Callers must hold
// the mutex.
func aliasOwner(sp *space, alias string) *backend.LocalImage {
	for _, img := range sp.images {
		if slices.Contains(img.Aliases, alias) {
			return img
		}
	}
	return nil
}
