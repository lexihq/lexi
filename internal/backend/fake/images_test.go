package fake

import (
	"errors"
	"slices"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
	before, _ := b.ListLocalImages(ctx())
	if err := b.PublishImage(ctx(), "src", ""); err != nil {
		t.Fatalf("publish without alias: %v", err)
	}
	after, _ := b.ListLocalImages(ctx())
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
	imgs, _ := b.ListLocalImages(ctx())
	if err := b.DeleteImage(ctx(), imgs[0].Fingerprint); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, _ := b.ListLocalImages(ctx())
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
	imgs, _ := b.ListLocalImages(ctx())
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
	imgs, _ := b.ListLocalImages(ctx())
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
