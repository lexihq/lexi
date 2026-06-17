package fake

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
)

// ExportInstance writes a deterministic backup blob for an existing instance so
// handler tests can exercise the download path (and the C2 import round-trip)
// without a daemon.
func (f *Fake) ExportInstance(ctx context.Context, name string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	_, err := io.WriteString(w, fakeBackupMagic+in.Image)
	return err
}

// ImportInstance recreates an instance from a blob ExportInstance wrote. It
// validates the magic header (rejecting foreign data with ErrInvalid) and
// recovers the original image so the export→import round-trip is observable.
func (f *Fake) ImportInstance(ctx context.Context, name string, r io.Reader) error {
	blob, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	image, ok := strings.CutPrefix(string(blob), fakeBackupMagic)
	if !ok {
		return fmt.Errorf("not a lexi backup: %w", backend.ErrInvalid)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	if _, ok := sp.instances[name]; ok {
		return conflict("instance %q already exists", name)
	}
	sp.instances[name] = &instance{
		Instance: backend.Instance{
			Name:      name,
			Status:    "Stopped",
			Image:     image,
			CreatedAt: f.now(),
		},
		files: seedFiles(name),
	}
	return nil
}
