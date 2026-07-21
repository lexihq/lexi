package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/metrics"
	"github.com/lexihq/lexi/internal/ui"
)

// metrics renders the self-refreshing live-metrics panel for an instance.
func (h handlers) metrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, err := h.backend.Metrics(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.MetricsPanel(name, m))
}

// metricsSeries returns the retained metrics history for an instance as JSON
// for the charts. Each request also appends the current live sample, so the
// history of the active scope accumulates (and survives page reloads) even
// when the background sampler is sampling a different remote/project. In the
// scope the sampler does cover, the store's minimum sample gap drops this
// handler's interleaved appends, so double-polling cannot halve the window.
func (h handlers) metricsSeries(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, err := h.backend.Metrics(r.Context(), name)
	if err != nil {
		// This endpoint is consumed by fetch(), not HTMX, so answer with JSON
		// (not the HTML error toast h.fail renders) and the matching status.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusFor(err))
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	key := metrics.Key(r.Context(), name)
	h.samples.Append(key, backend.MetricSample{Time: time.Now(), Metrics: m})

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, seriesJSON(h.samples.Series(key)))
}

// writeJSON encodes v to w, logging at Debug on failure: once the body write
// starts the status is already sent, and the only realistic cause is the client
// disconnecting mid-write (e.g. navigating away), which is routine.
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("encode metrics series", "err", err)
	}
}

// metricsSeriesData is the column-oriented shape uPlot consumes: parallel
// arrays indexed by sample, x-axis (t) in unix seconds. CPU entries are null
// when the sample's CPU% was unknown (the driver's first reading of an
// instance) — uPlot renders null as a gap, not a fake 0%.
type metricsSeriesData struct {
	T        []int64    `json:"t"`
	CPU      []*float64 `json:"cpu"`
	MemUsed  []int64    `json:"memUsed"`
	MemTotal []int64    `json:"memTotal"`
	Rx       []int64    `json:"rx"`
	Tx       []int64    `json:"tx"`
}

func seriesJSON(samples []backend.MetricSample) metricsSeriesData {
	d := metricsSeriesData{
		T:        make([]int64, len(samples)),
		CPU:      make([]*float64, len(samples)),
		MemUsed:  make([]int64, len(samples)),
		MemTotal: make([]int64, len(samples)),
		Rx:       make([]int64, len(samples)),
		Tx:       make([]int64, len(samples)),
	}
	for i, s := range samples {
		d.T[i] = s.Time.Unix()
		d.CPU[i] = s.CPUPercent
		d.MemUsed[i] = s.MemoryUsage
		d.MemTotal[i] = s.MemoryTotal
		d.Rx[i] = s.NetworkRx
		d.Tx[i] = s.NetworkTx
	}
	return d
}
