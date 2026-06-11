package incus

import (
	"archive/zip"
	"bufio"
	"bytes"
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
	"github.com/lxc/incus/v6/shared/cancel"
)

// contextReadWriteSeeker is contextWriteSeeker's sibling for the image
// download target, which the client requires to also be readable.
type contextReadWriteSeeker struct {
	io.ReadWriteSeeker

	ctx context.Context
}

func (w contextReadWriteSeeker) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.ReadWriteSeeker.Write(p)
}

// ListImages returns the full simplestreams catalog (one entry per alias), served
// from a lazy, mutex-guarded cache so the search UI can filter without refetching.
func (b *incusBackend) ListImages(ctx context.Context) ([]backend.Image, error) {
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
func (b *incusBackend) ListLocalImages(ctx context.Context) ([]backend.LocalImage, error) {
	images, err := b.project(ctx).GetImages()
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
			Fingerprint:     img.Fingerprint,
			Aliases:         aliases,
			Description:     img.Properties["description"],
			Arch:            img.Architecture,
			SizeBytes:       img.Size,
			Type:            img.Type,
			CreatedAt:       img.CreatedAt,
			Public:          img.Public,
			AutoUpdate:      img.AutoUpdate,
			ExpiresAt:       normalizeImageExpiry(img.ExpiresAt),
			HasUpdateSource: img.UpdateSource != nil,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out, nil
}

// PublishImage creates a local image from the (stopped; Incus enforces it)
// instance, then tags it with alias when one is given.
func (b *incusBackend) PublishImage(ctx context.Context, instance, alias string) error {
	op, err := b.project(ctx).CreateImage(api.ImagesPost{
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
	op, err := b.project(ctx).CopyImage(is, *img, &incusclient.ImageCopyArgs{CopyAliases: true})
	if err != nil {
		return fmt.Errorf("copy image %q: %w", alias, mapErr(err))
	}
	if err := waitRemoteOperation(ctx, op); err != nil {
		return fmt.Errorf("copy image %q: %w", alias, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteImage(ctx context.Context, fingerprint string) error {
	op, err := b.project(ctx).DeleteImage(fingerprint)
	return waitOp(ctx, op, err, "delete image %q", fingerprint)
}

func (b *incusBackend) AddImageAlias(ctx context.Context, fingerprint, alias string) error {
	err := b.project(ctx).CreateImageAlias(api.ImageAliasesPost{
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

func (b *incusBackend) RemoveImageAlias(ctx context.Context, alias string) error {
	if err := b.project(ctx).DeleteImageAlias(alias); err != nil {
		return fmt.Errorf("delete image alias %q: %w", alias, mapErr(err))
	}
	return nil
}

// spoolReadCloser streams a spooled export; Close releases the temp files.
type spoolReadCloser struct {
	io.Reader

	temps []*os.File
}

func (s spoolReadCloser) Close() error {
	for _, tmp := range s.temps {
		cleanupExportTemp(tmp)
	}
	return nil
}

// ExportImage downloads the image into temp spools (the client demands
// io.ReadWriteSeeker targets) and returns the download filename plus a reader
// over the result: the meta tarball as-is for unified images (named by the
// daemon, compression extension included), or a metadata+rootfs zip for split
// images (RootfsName on the response is only set when a rootfs part arrived).
// The filename is known before the first payload byte so callers can pick
// headers; the download is cancelable via ctx, like ExportInstance.
func (b *incusBackend) ExportImage(ctx context.Context, fingerprint string) (string, io.ReadCloser, error) {
	// The image type names the zip rootfs entry (rootfs vs rootfs.img — the
	// daemon's own import naming); the GET also 404s a ghost fingerprint
	// before any download work.
	img, _, err := b.project(ctx).GetImage(fingerprint)
	if err != nil {
		return "", nil, fmt.Errorf("get image %q: %w", fingerprint, mapErr(err))
	}

	var temps []*os.File
	cleanup := func() {
		for _, tmp := range temps {
			cleanupExportTemp(tmp)
		}
	}
	newTemp := func(pattern string) (*os.File, error) {
		tmp, err := os.CreateTemp("", pattern)
		if err == nil {
			temps = append(temps, tmp)
		}
		return tmp, err
	}

	meta, err := newTemp("lxcon-image-export-meta-*")
	if err != nil {
		return "", nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	rootfs, err := newTemp("lxcon-image-export-rootfs-*")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}

	if err := ctx.Err(); err != nil {
		cleanup()
		return "", nil, err
	}
	canceler := cancel.NewHTTPRequestCanceller()
	stopCancel := context.AfterFunc(ctx, func() {
		if err := canceler.Cancel(); err != nil && canceler.Cancelable() {
			slog.Warn("cancel image download", "image", fingerprint, "err", err)
		}
	})
	defer stopCancel()

	resp, err := b.project(ctx).GetImageFile(fingerprint, incusclient.ImageFileRequest{
		MetaFile:   contextReadWriteSeeker{ctx: ctx, ReadWriteSeeker: meta},
		RootfsFile: contextReadWriteSeeker{ctx: ctx, ReadWriteSeeker: rootfs},
		Canceler:   canceler,
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("export image %q: %w", fingerprint, mapErr(err))
	}
	if _, err := meta.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}

	if resp.RootfsName == "" {
		// Unified: the rootfs spool is unused; the reader owns both temps.
		// The daemon names the payload "<fingerprint><ext>" with the real
		// compression extension; fall back to a bare .tar if it didn't.
		filename := resp.MetaName
		if filename == "" {
			filename = shortFingerprint(fingerprint) + ".tar"
		}
		return filename, spoolReadCloser{Reader: meta, temps: temps}, nil
	}

	zipped, err := b.zipSplitImage(fingerprint, img.Type, meta, rootfs, newTemp)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return shortFingerprint(fingerprint) + ".zip", spoolReadCloser{Reader: zipped, temps: temps}, nil
}

// shortFingerprint trims a fingerprint to the familiar 12-char display form.
func shortFingerprint(fingerprint string) string {
	if len(fingerprint) > 12 {
		return fingerprint[:12]
	}
	return fingerprint
}

// zipSplitImage assembles the lxcon split-image packaging: a zip (spooled to
// another temp file) with a "metadata" entry plus "rootfs" or "rootfs.img"
// per the image type. Entries are stored, not deflated — the payloads are
// already compressed, and ImportImage rejects compressed entries as a
// zip-bomb guard.
func (b *incusBackend) zipSplitImage(fingerprint, imgType string, meta, rootfs *os.File, newTemp func(string) (*os.File, error)) (*os.File, error) {
	if _, err := rootfs.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	zipTmp, err := newTemp("lxcon-image-export-zip-*")
	if err != nil {
		return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}

	rootfsEntry := "rootfs"
	if imgType == "virtual-machine" {
		rootfsEntry = "rootfs.img"
	}
	zw := zip.NewWriter(zipTmp)
	for _, e := range []struct {
		name string
		src  io.Reader
	}{{"metadata", meta}, {rootfsEntry, rootfs}} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		if err != nil {
			return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
		}
		if _, err := io.Copy(w, e.src); err != nil {
			return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	if _, err := zipTmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("export image %q: %w", fingerprint, err)
	}
	return zipTmp, nil
}

// zipMagic is the local-file-header signature every zip stream starts with.
var zipMagic = []byte("PK\x03\x04")

// splitImageArgs spools a split-image zip to a temp file (zip reading needs
// io.ReaderAt) and returns CreateImage args streaming its metadata and rootfs
// entries, with the type the rootfs entry name encodes. Entries must be the
// exact lxcon packaging and stored uncompressed (zip-bomb guard: stored
// entries cannot expand past the upload cap). The release func frees the
// spool; call it once CreateImage has consumed the readers.
func (b *incusBackend) splitImageArgs(ctx context.Context, r io.Reader) (*incusclient.ImageCreateArgs, func(), error) {
	tmp, err := os.CreateTemp("", "lxcon-image-import-*")
	if err != nil {
		return nil, nil, fmt.Errorf("import image: %w", err)
	}
	release := func() { cleanupExportTemp(tmp) }

	size, err := io.Copy(tmp, contextReader{ctx: ctx, Reader: r})
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("import image: %w", err)
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("import image: corrupt split-image zip: %w", backend.ErrInvalid)
	}

	var metaEntry, rootfsEntry *zip.File
	imgType := ""
	for _, zf := range zr.File {
		if zf.Method != zip.Store {
			release()
			return nil, nil, fmt.Errorf("import image: split-image zip entry %q is compressed: %w", zf.Name, backend.ErrInvalid)
		}
		switch zf.Name {
		case "metadata":
			metaEntry = zf
		case "rootfs":
			rootfsEntry, imgType = zf, "container"
		case "rootfs.img":
			rootfsEntry, imgType = zf, "virtual-machine"
		default:
			release()
			return nil, nil, fmt.Errorf("import image: unexpected split-image zip entry %q: %w", zf.Name, backend.ErrInvalid)
		}
	}
	if metaEntry == nil || rootfsEntry == nil {
		release()
		return nil, nil, fmt.Errorf("import image: split-image zip is missing metadata or rootfs: %w", backend.ErrInvalid)
	}

	metaRC, err := metaEntry.Open()
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("import image: corrupt split-image zip: %w", backend.ErrInvalid)
	}
	rootfsRC, err := rootfsEntry.Open()
	if err != nil {
		closeAndLogFile("split-image metadata entry", metaRC)
		release()
		return nil, nil, fmt.Errorf("import image: corrupt split-image zip: %w", backend.ErrInvalid)
	}
	releaseAll := func() {
		closeAndLogFile("split-image metadata entry", metaRC)
		closeAndLogFile("split-image rootfs entry", rootfsRC)
		release()
	}
	return &incusclient.ImageCreateArgs{
		MetaFile:   contextReader{ctx: ctx, Reader: metaRC},
		MetaName:   "metadata",
		RootfsFile: contextReader{ctx: ctx, Reader: rootfsRC},
		RootfsName: rootfsEntry.Name,
		Type:       imgType,
	}, releaseAll, nil
}

// ImportImage creates a local image from a unified tarball or a lxcon
// split-image zip (detected by the zip signature; the rootfs entry name
// carries the image type), tagging it with alias when given (a failed alias
// rolls the import back, like PublishImage). The upload reader is
// context-aware so an aborted request stops mid-stream.
func (b *incusBackend) ImportImage(ctx context.Context, r io.Reader, alias string) error {
	br := bufio.NewReader(r)
	sig, err := br.Peek(len(zipMagic))
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("import image: %w", err)
	}

	args := &incusclient.ImageCreateArgs{MetaFile: contextReader{ctx: ctx, Reader: br}}
	if bytes.Equal(sig, zipMagic) {
		splitArgs, release, err := b.splitImageArgs(ctx, br)
		if err != nil {
			return err
		}
		defer release()
		args = splitArgs
	}

	op, err := b.project(ctx).CreateImage(api.ImagesPost{}, args)
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
// normalizeImageExpiry maps the daemon sentinel for "never expires" (epoch
// or earlier) to the zero time the domain uses.
func normalizeImageExpiry(t time.Time) time.Time {
	if t.Unix() <= 0 {
		return time.Time{}
	}
	return t
}

func (b *incusBackend) UpdateImage(ctx context.Context, fingerprint string, edit backend.ImageEdit) error {
	img, etag, err := b.project(ctx).GetImage(fingerprint)
	if err != nil {
		return fmt.Errorf("get image %q: %w", fingerprint, mapErr(err))
	}
	put := img.Writable()
	if put.Properties == nil {
		put.Properties = map[string]string{}
	}
	put.Properties["description"] = edit.Description
	put.Public = edit.Public
	put.AutoUpdate = edit.AutoUpdate
	put.ExpiresAt = edit.ExpiresAt
	if err := b.project(ctx).UpdateImage(fingerprint, put, etag); err != nil {
		return fmt.Errorf("update image %q: %w", fingerprint, mapErr(err))
	}
	return nil
}

// RefreshImage asks the daemon to re-pull the image from its update source;
// the daemon rejects images without one (mapped to ErrInvalid).
func (b *incusBackend) RefreshImage(ctx context.Context, fingerprint string) error {
	op, err := b.project(ctx).RefreshImage(fingerprint)
	if err != nil {
		return fmt.Errorf("refresh image %q: %w", fingerprint, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("refresh image %q: %w", fingerprint, mapErr(err))
	}
	return nil
}
