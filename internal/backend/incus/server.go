package incus

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"maps"
	"sort"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

// GetServerOverview combines the daemon environment with the host's headline
// resources (CPU threads, memory).
func (b *incusBackend) GetServerOverview(ctx context.Context) (backend.ServerOverview, error) {
	srv, _, err := b.server(ctx).GetServer()
	if err != nil {
		return backend.ServerOverview{}, fmt.Errorf("get server: %w", mapErr(err))
	}
	res, err := b.server(ctx).GetServerResources()
	if err != nil {
		return backend.ServerOverview{}, fmt.Errorf("get server resources: %w", mapErr(err))
	}
	env := srv.Environment
	return backend.ServerOverview{
		ServerVersion: env.ServerVersion,
		Kernel:        env.Kernel,
		KernelVersion: env.KernelVersion,
		Driver:        env.Driver,
		DriverVersion: env.DriverVersion,
		CPUThreads:    int(res.CPU.Total),      //nolint:gosec // G115: CPU thread counts are tiny.
		MemoryUsed:    int64(res.Memory.Used),  //nolint:gosec // G115: real memory sizes fit int64.
		MemoryTotal:   int64(res.Memory.Total), //nolint:gosec // G115: real memory sizes fit int64.
	}, nil
}

// GetServerConfig returns the config map plus the server etag as the opaque
// version token callers thread back into UpdateServerConfig.
func (b *incusBackend) GetServerConfig(ctx context.Context) (map[string]string, string, error) {
	srv, etag, err := b.server(ctx).GetServer()
	if err != nil {
		return nil, "", fmt.Errorf("get server config: %w", mapErr(err))
	}
	return maps.Clone(map[string]string(srv.Config)), etag, nil
}

// UpdateServerConfig replaces the server config map. The version is the etag
// from GetServerConfig: the daemon rejects the PUT with 412 (mapped to
// ErrConflict) when the config changed since that read. An empty version sends
// no If-Match and updates unconditionally.
//
// The daemon does not treat a key omitted from the PUT as a removal — unset
// means "explicitly set to empty" (what `incus config unset` sends) — so keys
// present on the server but absent from config are added with empty values to
// make this a true replace.
func (b *incusBackend) UpdateServerConfig(ctx context.Context, config map[string]string, version string) error {
	srv, _, err := b.server(ctx).GetServer()
	if err != nil {
		return fmt.Errorf("get server: %w", mapErr(err))
	}
	put := make(api.ConfigMap, len(config)+len(srv.Config))
	for k := range srv.Config {
		if _, kept := config[k]; !kept {
			put[k] = "" // removal marker
		}
	}
	maps.Copy(put, api.ConfigMap(config))
	if err := b.server(ctx).UpdateServer(api.ServerPut{Config: put}, version); err != nil {
		return fmt.Errorf("update server config: %w", mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListCertificates(ctx context.Context) ([]backend.Certificate, error) {
	certs, err := b.server(ctx).GetCertificates()
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", mapErr(err))
	}
	out := make([]backend.Certificate, 0, len(certs))
	for _, c := range certs {
		out = append(out, backend.Certificate{
			Name:        c.Name,
			Type:        c.Type,
			Fingerprint: c.Fingerprint,
			Restricted:  c.Restricted,
			Projects:    c.Projects,
		})
	}
	return out, nil
}

// AddCertificate decodes the pasted PEM and hands the daemon the base64 DER;
// the daemon is authoritative for X.509 validity and the certificate type.
func (b *incusBackend) AddCertificate(ctx context.Context, name, certType, pemData string) error {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("add certificate %q: not a PEM certificate: %w", name, backend.ErrInvalid)
	}
	post := api.CertificatesPost{
		CertificatePut: api.CertificatePut{
			Name:        name,
			Type:        certType,
			Certificate: base64.StdEncoding.EncodeToString(block.Bytes),
		},
	}
	if err := b.server(ctx).CreateCertificate(post); err != nil {
		return fmt.Errorf("add certificate %q: %w", name, mapErr(err))
	}
	return nil
}

// DeleteCertificate removes a certificate from the trust store by fingerprint.
func (b *incusBackend) DeleteCertificate(ctx context.Context, fingerprint string) error {
	if err := b.server(ctx).DeleteCertificate(fingerprint); err != nil {
		return fmt.Errorf("delete certificate %q: %w", fingerprint, mapErr(err))
	}
	return nil
}

// UpdateCertificate renames a trusted certificate and sets its project
// restriction via read-modify-write: the cert body and type are preserved and
// the read etag makes the update conditional on no concurrent change.
func (b *incusBackend) UpdateCertificate(ctx context.Context, fingerprint, name string, restricted bool, projects []string) error {
	if name == "" {
		return fmt.Errorf("certificate name is required: %w", backend.ErrInvalid)
	}
	cert, etag, err := b.server(ctx).GetCertificate(fingerprint)
	if err != nil {
		return fmt.Errorf("get certificate %q: %w", fingerprint, mapErr(err))
	}
	put := cert.Writable()
	put.Name = name
	put.Restricted = restricted
	if restricted {
		put.Projects = projects
	} else {
		put.Projects = nil
	}
	if err := b.server(ctx).UpdateCertificate(fingerprint, put, etag); err != nil {
		return fmt.Errorf("update certificate %q: %w", fingerprint, mapErr(err))
	}
	return nil
}

// ListWarnings returns daemon warnings, newest last-seen first.
func (b *incusBackend) ListWarnings(ctx context.Context) ([]backend.Warning, error) {
	warnings, err := b.server(ctx).GetWarnings()
	if err != nil {
		return nil, fmt.Errorf("list warnings: %w", mapErr(err))
	}
	out := make([]backend.Warning, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, backend.Warning{
			UUID:        w.UUID,
			Type:        w.Type,
			Severity:    w.Severity,
			Status:      w.Status,
			Count:       w.Count,
			LastMessage: w.LastMessage,
			LastSeenAt:  w.LastSeenAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

// AcknowledgeWarning flips a warning's status via a conditional PUT (there is
// no PATCH for warnings).
func (b *incusBackend) AcknowledgeWarning(ctx context.Context, uuid string) error {
	_, etag, err := b.server(ctx).GetWarning(uuid)
	if err != nil {
		return fmt.Errorf("get warning %q: %w", uuid, mapErr(err))
	}
	if err := b.server(ctx).UpdateWarning(uuid, api.WarningPut{Status: "acknowledged"}, etag); err != nil {
		return fmt.Errorf("acknowledge warning %q: %w", uuid, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteWarning(ctx context.Context, uuid string) error {
	if err := b.server(ctx).DeleteWarning(uuid); err != nil {
		return fmt.Errorf("delete warning %q: %w", uuid, mapErr(err))
	}
	return nil
}
