package incus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// ListImages returns the full simplestreams catalog (one entry per alias), served
// from a lazy, mutex-guarded cache so the search UI can filter without refetching.
func (b *incusBackend) ListImages(_ context.Context) ([]backend.Image, error) {
	b.imgMu.Lock()
	defer b.imgMu.Unlock()

	if b.imgCache != nil && time.Now().Before(b.imgExpiry) {
		return append([]backend.Image(nil), b.imgCache...), nil
	}

	is, err := incusclient.ConnectSimpleStreams(imagesRemote, nil)
	if err != nil {
		return nil, fmt.Errorf("connect images remote: %w", err)
	}
	images, err := is.GetImages()
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	b.imgCache = toImages(images)
	b.imgExpiry = time.Now().Add(imageCacheTTL)
	return append([]backend.Image(nil), b.imgCache...), nil
}

// toImages flattens the simplestreams catalog into one launchable domain Image
// per (alias, architecture, type), pulling filter fields from image properties.
func toImages(images []api.Image) []backend.Image {
	seen := make(map[string]bool)
	out := make([]backend.Image, 0, len(images))
	for i := range images {
		img := &images[i]
		for _, al := range img.Aliases {
			if al.Name == "" {
				continue
			}
			key := al.Name + "\x00" + img.Architecture + "\x00" + img.Type
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, backend.Image{
				Alias:        al.Name,
				Fingerprint:  img.Fingerprint,
				Description:  firstNonEmpty(al.Description, img.Properties["description"]),
				Arch:         img.Architecture,
				SizeBytes:    img.Size,
				Distribution: strings.ToLower(firstNonEmpty(img.Properties["os"], distroFromAlias(al.Name))),
				Release:      img.Properties["release"],
				Variant:      img.Properties["variant"],
				Type:         img.Type,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Alias != out[j].Alias {
			return out[i].Alias < out[j].Alias
		}
		if out[i].Arch != out[j].Arch {
			return out[i].Arch < out[j].Arch
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// distroFromAlias falls back to the first path segment of an alias (e.g.
// "debian" from "debian/12") when the image carries no os property.
func distroFromAlias(alias string) string {
	distro, _, _ := strings.Cut(alias, "/")
	return distro
}

// ListLocalImages returns the daemon's local image store.
func (b *incusBackend) ListLocalImages(_ context.Context) ([]backend.LocalImage, error) {
	images, err := b.srv.GetImages()
	if err != nil {
		return nil, fmt.Errorf("list local images: %w", mapErr(err))
	}
	out := make([]backend.LocalImage, 0, len(images))
	for i := range images {
		img := &images[i]
		aliases := make([]string, 0, len(img.Aliases))
		for _, al := range img.Aliases {
			aliases = append(aliases, al.Name)
		}
		out = append(out, backend.LocalImage{
			Fingerprint: img.Fingerprint,
			Aliases:     aliases,
			Description: img.Properties["description"],
			Arch:        img.Architecture,
			SizeBytes:   img.Size,
			Type:        img.Type,
			CreatedAt:   img.CreatedAt,
			Public:      img.Public,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out, nil
}

// PublishImage creates a local image from the (stopped; Incus enforces it)
// instance, then tags it with alias when one is given.
func (b *incusBackend) PublishImage(ctx context.Context, instance, alias string) error {
	op, err := b.srv.CreateImage(api.ImagesPost{
		Source: &api.ImagesPostSource{Type: "instance", Name: instance},
	}, nil)
	if err := waitOp(ctx, op, err, "publish image from %q", instance); err != nil {
		return err
	}
	if alias == "" {
		return nil
	}
	fp, ok := op.Get().Metadata["fingerprint"].(string)
	if !ok || fp == "" {
		return fmt.Errorf("publish image from %q: operation returned no fingerprint", instance)
	}
	if err := b.AddImageAlias(ctx, fp, alias); err != nil {
		// Roll the publish back so a failed alias (e.g. a duplicate) doesn't
		// leave an orphaned, unaliased image in the store.
		if derr := b.DeleteImage(ctx, fp); derr != nil {
			return errors.Join(err, fmt.Errorf("rollback published image %q: %w", fp, derr))
		}
		return err
	}
	return nil
}

// CopyImage pulls the image behind alias from the images remote into the local
// store, copying its aliases along.
func (b *incusBackend) CopyImage(ctx context.Context, alias string) error {
	is, err := incusclient.ConnectSimpleStreams(imagesRemote, nil)
	if err != nil {
		return fmt.Errorf("connect images remote: %w", err)
	}
	return b.copyImageFrom(ctx, is, alias)
}

func (b *incusBackend) copyImageFrom(ctx context.Context, is incusclient.ImageServer, alias string) error {
	entry, _, err := is.GetImageAlias(alias)
	if err != nil {
		return fmt.Errorf("resolve image alias %q: %w", alias, mapErr(err))
	}
	img, _, err := is.GetImage(entry.Target)
	if err != nil {
		return fmt.Errorf("get remote image %q: %w", alias, mapErr(err))
	}
	op, err := b.srv.CopyImage(is, *img, &incusclient.ImageCopyArgs{CopyAliases: true})
	if err != nil {
		return fmt.Errorf("copy image %q: %w", alias, mapErr(err))
	}
	if err := waitRemoteOperation(ctx, op); err != nil {
		return fmt.Errorf("copy image %q: %w", alias, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteImage(ctx context.Context, fingerprint string) error {
	op, err := b.srv.DeleteImage(fingerprint)
	return waitOp(ctx, op, err, "delete image %q", fingerprint)
}

func (b *incusBackend) AddImageAlias(_ context.Context, fingerprint, alias string) error {
	err := b.srv.CreateImageAlias(api.ImageAliasesPost{
		ImageAliasesEntry: api.ImageAliasesEntry{
			Name:                 alias,
			ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: fingerprint},
		},
	})
	if err != nil {
		return fmt.Errorf("create image alias %q: %w", alias, mapErr(err))
	}
	return nil
}

func (b *incusBackend) RemoveImageAlias(_ context.Context, alias string) error {
	if err := b.srv.DeleteImageAlias(alias); err != nil {
		return fmt.Errorf("delete image alias %q: %w", alias, mapErr(err))
	}
	return nil
}

// ExportImage streams the image tarball to w. The client demands an
// io.ReadWriteSeeker target, so the download spools through a temp file before
// being copied out. Split images (separate metadata + rootfs; the client
// refuses them when no rootfs target is given) are ErrUnsupported — a browser
// download is a single file.
func (b *incusBackend) ExportImage(_ context.Context, fingerprint string, w io.Writer) error {
	tmp, err := os.CreateTemp("", "lxcon-image-export-*")
	if err != nil {
		return fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	defer func() {
		if cerr := tmp.Close(); cerr != nil {
			slog.Warn("close image export spool", "image", fingerprint, "err", cerr)
		}
		if rerr := os.Remove(tmp.Name()); rerr != nil {
			slog.Warn("remove image export spool", "image", fingerprint, "err", rerr)
		}
	}()

	if _, err := b.srv.GetImageFile(fingerprint, incusclient.ImageFileRequest{MetaFile: tmp}); err != nil {
		if strings.Contains(err.Error(), "Multi-part image") {
			return fmt.Errorf("image %q is a split image (metadata + rootfs) and cannot be downloaded as one file: %w", fingerprint, backend.ErrUnsupported)
		}
		return fmt.Errorf("export image %q: %w", fingerprint, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	return nil
}

// ImportImage creates a local image from a unified tarball, tagging it with
// alias when given (a failed alias rolls the import back, like PublishImage).
func (b *incusBackend) ImportImage(ctx context.Context, r io.Reader, alias string) error {
	op, err := b.srv.CreateImage(api.ImagesPost{}, &incusclient.ImageCreateArgs{MetaFile: r})
	if err := waitOp(ctx, op, err, "import image"); err != nil {
		return err
	}
	if alias == "" {
		return nil
	}
	fp, ok := op.Get().Metadata["fingerprint"].(string)
	if !ok || fp == "" {
		return errors.New("import image: operation returned no fingerprint")
	}
	if err := b.AddImageAlias(ctx, fp, alias); err != nil {
		if derr := b.DeleteImage(ctx, fp); derr != nil {
			return errors.Join(err, fmt.Errorf("rollback imported image %q: %w", fp, derr))
		}
		return err
	}
	return nil
}

// UpdateImage sets the description property and the public flag via
// GET-preserve-PUT with the fresh etag, so AutoUpdate/ExpiresAt/Profiles and
// the other properties survive (a PUT silently clears omitted fields).
func (b *incusBackend) UpdateImage(_ context.Context, fingerprint, description string, public bool) error {
	img, etag, err := b.srv.GetImage(fingerprint)
	if err != nil {
		return fmt.Errorf("get image %q: %w", fingerprint, mapErr(err))
	}
	put := img.Writable()
	if put.Properties == nil {
		put.Properties = map[string]string{}
	}
	put.Properties["description"] = description
	put.Public = public
	if err := b.srv.UpdateImage(fingerprint, put, etag); err != nil {
		return fmt.Errorf("update image %q: %w", fingerprint, mapErr(err))
	}
	return nil
}
