package fake

import "testing"

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
