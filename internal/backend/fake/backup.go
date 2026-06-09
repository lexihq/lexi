package fake

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// ExportInstance writes a deterministic backup blob for an existing instance so
// handler tests can exercise the download path (and the C2 import round-trip)
// without a daemon.
func (f *Fake) ExportInstance(_ context.Context, name string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	_, err := io.WriteString(w, fakeBackupMagic+in.Image)
	return err
}

// ImportInstance recreates an instance from a blob ExportInstance wrote. It
// validates the magic header (rejecting foreign data with ErrInvalid) and
// recovers the original image so the export→import round-trip is observable.
func (f *Fake) ImportInstance(_ context.Context, name string, r io.Reader) error {
	blob, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	image, ok := strings.CutPrefix(string(blob), fakeBackupMagic)
	if !ok {
		return fmt.Errorf("not a lxcon backup: %w", backend.ErrInvalid)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; ok {
		return conflict("instance %q already exists", name)
	}
	f.instances[name] = &instance{
		Instance: backend.Instance{
			Name:      name,
			Status:    "Stopped",
			Image:     image,
			CreatedAt: f.now(),
		},
	}
	return nil
}
