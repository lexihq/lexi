// Package static embeds the built CSS/JS assets so lxcon ships as a single
// self-contained binary (no external asset directory required at runtime).
package static

import "embed"

//go:embed css js
var FS embed.FS
