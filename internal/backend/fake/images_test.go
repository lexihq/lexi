package fake

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
)

func TestListImagesCurated(t *testing.T) {
	imgs, err := New().ListImages(ctx())
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("expected a curated image set, got none")
	}
	want := map[string]bool{"debian/12": false, "ubuntu/24.04": false, "alpine/edge": false}
	for _, img := range imgs {
		if _, ok := want[img.Alias]; ok {
			want[img.Alias] = true
		}
	}
	for alias, found := range want {
		if !found {
			t.Errorf("curated image %q missing from %+v", alias, imgs)
		}
	}
}

// mustLocal lists the local image store, failing the test on error.
func mustLocal(t *testing.T, b *Fake) []backend.LocalImage {
	t.Helper()
	imgs, err := b.ListLocalImages(ctx())
	if err != nil {
		t.Fatalf("list local images: %v", err)
	}
	return imgs
}

// findLocal returns the local image carrying alias, or nil.
func findLocal(t *testing.T, b *Fake, alias string) *backend.LocalImage {
	t.Helper()
	imgs, err := b.ListLocalImages(ctx())
	if err != nil {
		t.Fatalf("list local images: %v", err)
	}
	for i := range imgs {
		if slices.Contains(imgs[i].Aliases, alias) {
			return &imgs[i]
		}
	}
	return nil
}

func TestListLocalImagesSeeded(t *testing.T) {
	imgs, err := New().ListLocalImages(ctx())
	if err != nil {
		t.Fatalf("list local images: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("expected a seeded local image, got none")
	}
	if imgs[0].Fingerprint == "" {
		t.Errorf("seeded image missing fingerprint: %+v", imgs[0])
	}
}

func TestPublishImage(t *testing.T) {
	b := New()
	mustCreate(t, b, "src")
	if err := b.PublishImage(ctx(), "src", "my-snap"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	img := findLocal(t, b, "my-snap")
	if img == nil {
		t.Fatal("published image not in local list")
	}
	if img.Fingerprint == "" {
		t.Error("published image has no fingerprint")
	}
}

func TestPublishImageNoAlias(t *testing.T) {
	b := New()
	mustCreate(t, b, "src")
	before := mustLocal(t, b)
	if err := b.PublishImage(ctx(), "src", ""); err != nil {
		t.Fatalf("publish without alias: %v", err)
	}
	after := mustLocal(t, b)
	if len(after) != len(before)+1 {
		t.Fatalf("expected %d local images, got %d", len(before)+1, len(after))
	}
}

func TestPublishImageGhostInstance(t *testing.T) {
	err := New().PublishImage(ctx(), "ghost", "x")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPublishImageAliasConflict(t *testing.T) {
	b := New()
	mustCreate(t, b, "src")
	if err := b.PublishImage(ctx(), "src", "dup"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	err := b.PublishImage(ctx(), "src", "dup")
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestCopyImage(t *testing.T) {
	b := New()
	if err := b.CopyImage(ctx(), "alpine/edge"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if findLocal(t, b, "alpine/edge") == nil {
		t.Fatal("copied image not in local list")
	}
}

func TestCopyImageGhostAlias(t *testing.T) {
	err := New().CopyImage(ctx(), "no/such")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCopyImageAlreadyLocal(t *testing.T) {
	b := New()
	if err := b.CopyImage(ctx(), "alpine/edge"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	err := b.CopyImage(ctx(), "alpine/edge")
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestDeleteImage(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	if err := b.DeleteImage(ctx(), imgs[0].Fingerprint); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after := mustLocal(t, b)
	if len(after) != len(imgs)-1 {
		t.Fatalf("expected %d local images after delete, got %d", len(imgs)-1, len(after))
	}
}

func TestDeleteImageGhost(t *testing.T) {
	err := New().DeleteImage(ctx(), "no-such-fp")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAddRemoveImageAlias(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	fp := imgs[0].Fingerprint
	if err := b.AddImageAlias(ctx(), fp, "extra"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	if findLocal(t, b, "extra") == nil {
		t.Fatal("added alias not visible")
	}
	if err := b.RemoveImageAlias(ctx(), "extra"); err != nil {
		t.Fatalf("remove alias: %v", err)
	}
	if findLocal(t, b, "extra") != nil {
		t.Fatal("removed alias still visible")
	}
}

func TestAddImageAliasGhostImage(t *testing.T) {
	err := New().AddImageAlias(ctx(), "no-such-fp", "x")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAddImageAliasDuplicate(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	fp := imgs[0].Fingerprint
	if err := b.AddImageAlias(ctx(), fp, "dup"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	err := b.AddImageAlias(ctx(), fp, "dup")
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestRemoveImageAliasGhost(t *testing.T) {
	err := New().RemoveImageAlias(ctx(), "no-such-alias")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// exportImageBlob exports through the spool-then-stream shape and returns
// the download filename plus the payload bytes.
func exportImageBlob(t *testing.T, b *Fake, fp string) (string, []byte) {
	t.Helper()
	filename, rc, err := b.ExportImage(ctx(), fp)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	blob, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close export: %v", err)
	}
	return filename, blob
}

func TestExportImportImageRoundTrip(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	fp := imgs[0].Fingerprint

	filename, blob := exportImageBlob(t, b, fp)
	if !strings.HasSuffix(filename, ".tar") {
		t.Fatalf("want a tarball filename for a container image, got %q", filename)
	}
	if err := b.ImportImage(ctx(), bytes.NewReader(blob), "restored"); err != nil {
		t.Fatalf("import: %v", err)
	}

	after, err := b.ListLocalImages(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *backend.LocalImage
	for i := range after {
		if slices.Contains(after[i].Aliases, "restored") {
			found = &after[i]
		}
	}
	if found == nil || found.Fingerprint != "imported-"+fp {
		t.Fatalf("imported image missing or wrong fingerprint: %+v", found)
	}
}

func TestExportImageGhostIs404(t *testing.T) {
	_, _, err := New().ExportImage(ctx(), "ghost")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSplitImageExportImportRoundTrip(t *testing.T) {
	b := New()
	b.SeedSplitImage("fake-vm-img", "VM image")

	filename, blob := exportImageBlob(t, b, "fake-vm-img")
	if !strings.HasSuffix(filename, ".zip") {
		t.Fatalf("want a zip filename for a VM image, got %q", filename)
	}

	// The zip carries exactly metadata + rootfs.img, both stored uncompressed.
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("open export zip: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, zf := range zr.File {
		names = append(names, zf.Name)
		if zf.Method != zip.Store {
			t.Fatalf("entry %q is compressed", zf.Name)
		}
	}
	if !slices.Equal(names, []string{"metadata", "rootfs.img"}) {
		t.Fatalf("want [metadata rootfs.img], got %v", names)
	}

	// Importing the zip recovers a VM-type image under the derived fingerprint.
	if err := b.ImportImage(ctx(), bytes.NewReader(blob), "restored-vm"); err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, img := range mustLocal(t, b) {
		if img.Fingerprint == "imported-fake-vm-img" {
			if img.Type != "virtual-machine" {
				t.Fatalf("imported type = %q, want virtual-machine", img.Type)
			}
			return
		}
	}
	t.Fatal("imported split image not found")
}

func TestImportImageRejectsBadZips(t *testing.T) {
	b := New()

	// A zip that isn't ours (wrong entry name).
	var foreign bytes.Buffer
	zw := zip.NewWriter(&foreign)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "evil", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.ImportImage(ctx(), bytes.NewReader(foreign.Bytes()), ""); !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("foreign zip: want ErrInvalid, got %v", err)
	}

	// A compressed (Deflate) entry is refused even with the right names.
	var deflated bytes.Buffer
	zw = zip.NewWriter(&deflated)
	for _, name := range []string{"metadata", "rootfs.img"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(fakeImageMagic + "x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.ImportImage(ctx(), bytes.NewReader(deflated.Bytes()), ""); !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("deflated zip: want ErrInvalid, got %v", err)
	}
}

func TestImportImageForeignBlobIsInvalid(t *testing.T) {
	err := New().ImportImage(ctx(), strings.NewReader("not an image"), "")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestImportImageAliasConflict(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	fp := imgs[0].Fingerprint
	taken := imgs[0].Aliases[0]

	_, blob := exportImageBlob(t, b, fp)
	err := b.ImportImage(ctx(), bytes.NewReader(blob), taken)
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict for taken alias, got %v", err)
	}
}

func TestUpdateImageSetsDescriptionAndPublic(t *testing.T) {
	b := New()
	imgs := mustLocal(t, b)
	fp := imgs[0].Fingerprint

	if err := b.UpdateImage(ctx(), fp, backend.ImageEdit{Description: "edited", Public: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	after := mustLocal(t, b)
	for _, img := range after {
		if img.Fingerprint == fp {
			if img.Description != "edited" || !img.Public {
				t.Fatalf("update not applied: %+v", img)
			}
			return
		}
	}
	t.Fatal("image disappeared")
}

func TestUpdateImageGhostIs404(t *testing.T) {
	err := New().UpdateImage(ctx(), "ghost", backend.ImageEdit{Description: "x"})
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
