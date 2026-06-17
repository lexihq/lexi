package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"maps"
	"sort"
	"strconv"

	"github.com/lexihq/lexi/internal/backend"
)

func (f *Fake) GetServerOverview(ctx context.Context) (backend.ServerOverview, error) {
	return backend.ServerOverview{
		ServerVersion: "6.0-fake",
		Kernel:        "Linux",
		KernelVersion: "6.1.0-fake",
		Driver:        "fake",
		DriverVersion: "1.0",
		CPUThreads:    8,
		MemoryUsed:    4 << 30,
		MemoryTotal:   16 << 30,
	}, nil
}

func (f *Fake) GetServerHardware(ctx context.Context) (backend.ServerHardware, error) {
	return backend.ServerHardware{
		GPUs: []backend.GPUCard{
			{Vendor: "Fake Graphics", Product: "FakeGPU 1000", Driver: "fakegpu", PCIAddress: "0000:00:02.0"},
		},
		NICs: []backend.NetworkCard{
			{
				Vendor: "Fake Networks", Product: "FakeNIC 10G", Driver: "fakenic", PCIAddress: "0000:0d:00.0",
				Ports: []backend.NetworkPort{{ID: "eth0", Address: "00:16:3e:00:00:01"}},
			},
		},
		Disks: []backend.HostDisk{
			{ID: "nvme0n1", Model: "FAKE SSD 256", Type: "nvme", SizeBytes: 256 << 30},
			{ID: "sda", Model: "FAKE USB 64", Type: "usb", SizeBytes: 64 << 30, Removable: true},
		},
	}, nil
}

func (f *Fake) GetServerConfig(ctx context.Context) (map[string]string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return maps.Clone(f.remote(ctx).serverConfig), strconv.Itoa(f.remote(ctx).serverConfigVersion), nil
}

func (f *Fake) UpdateServerConfig(ctx context.Context, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.remote(ctx).serverConfigVersion) {
		return conflict("server config version %s", version)
	}
	f.remote(ctx).serverConfig = maps.Clone(config)
	if f.remote(ctx).serverConfig == nil {
		f.remote(ctx).serverConfig = map[string]string{}
	}
	f.remote(ctx).serverConfigVersion++
	return nil
}

func (f *Fake) ListCertificates(ctx context.Context) ([]backend.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]backend.Certificate(nil), f.remote(ctx).certificates...), nil
}

func (f *Fake) AddCertificate(ctx context.Context, name, certType, pemData string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	block, _ := pem.Decode([]byte(pemData))
	if block == nil || block.Type != "CERTIFICATE" {
		return invalid("certificate %q: not a PEM certificate", name)
	}
	sum := sha256.Sum256(block.Bytes)
	fingerprint := hex.EncodeToString(sum[:])
	for _, c := range f.remote(ctx).certificates {
		if c.Fingerprint == fingerprint {
			return conflict("certificate %q already trusted", fingerprint)
		}
	}
	f.remote(ctx).certificates = append(f.remote(ctx).certificates, backend.Certificate{
		Name: name, Type: certType, Fingerprint: fingerprint,
	})
	return nil
}

func (f *Fake) DeleteCertificate(ctx context.Context, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, c := range f.remote(ctx).certificates {
		if c.Fingerprint == fingerprint {
			f.remote(ctx).certificates = append(f.remote(ctx).certificates[:i], f.remote(ctx).certificates[i+1:]...)
			return nil
		}
	}
	return notFoundf("certificate %q", fingerprint)
}

func (f *Fake) UpdateCertificate(ctx context.Context, fingerprint, name string, restricted bool, projects []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if name == "" {
		return invalid("certificate name is required")
	}
	for i, c := range f.remote(ctx).certificates {
		if c.Fingerprint == fingerprint {
			f.remote(ctx).certificates[i].Name = name
			f.remote(ctx).certificates[i].Restricted = restricted
			if !restricted {
				projects = nil
			}
			f.remote(ctx).certificates[i].Projects = append([]string(nil), projects...)
			return nil
		}
	}
	return notFoundf("certificate %q", fingerprint)
}

func (f *Fake) ListWarnings(ctx context.Context) ([]backend.Warning, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := append([]backend.Warning(nil), f.remote(ctx).warnings...)
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (f *Fake) DeleteWarning(ctx context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, w := range f.remote(ctx).warnings {
		if w.UUID == uuid {
			f.remote(ctx).warnings = append(f.remote(ctx).warnings[:i], f.remote(ctx).warnings[i+1:]...)
			return nil
		}
	}
	return notFoundf("warning %q", uuid)
}

func (f *Fake) AcknowledgeWarning(ctx context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.remote(ctx).warnings {
		if f.remote(ctx).warnings[i].UUID == uuid {
			f.remote(ctx).warnings[i].Status = "acknowledged"
			return nil
		}
	}
	return notFoundf("warning %q", uuid)
}
