package ui

import "github.com/lexihq/lexi/internal/backend"

// BulkAction describes one button in the instances bulk-action bar. BulkActions
// is the single source of truth for which bulk actions the UI offers and the
// toast verb each uses; the server keys its behavior map by the same Key and a
// test asserts the two stay in lockstep, so the bar and the handler can't drift.
type BulkAction struct {
	Key         string                          // form action value + behavior-map key
	Verb        string                          // past-tense, for the summary toast
	Label       string                          // button label
	Icon        string                          // lucide icon name
	Destructive bool                            // red styling + hx-confirm prompt
	Needs       func(backend.Capabilities) bool // nil → always available
}

// BulkActions is the ordered set of bulk lifecycle actions shown in the bar.
var BulkActions = []BulkAction{
	{Key: "start", Verb: "Started", Label: "Start", Icon: "circle-play"},
	{Key: "stop", Verb: "Stopped", Label: "Stop", Icon: "circle-stop"},
	{Key: "restart", Verb: "Restarted", Label: "Restart", Icon: "rotate-cw"},
	{Key: "snapshot", Verb: "Snapshotted", Label: "Snapshot", Icon: "camera", Needs: func(c backend.Capabilities) bool { return c.Snapshots }},
	{Key: "delete", Verb: "Deleted", Label: "Delete", Icon: "trash-2", Destructive: true},
}

// BulkActionByKey returns the action with the given key, if any.
func BulkActionByKey(key string) (BulkAction, bool) {
	for _, a := range BulkActions {
		if a.Key == key {
			return a, true
		}
	}
	return BulkAction{}, false
}
