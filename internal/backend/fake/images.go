package fake

import (
	"context"

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
