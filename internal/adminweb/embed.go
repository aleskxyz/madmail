// Package adminweb embeds the pre-built admin dashboard SPA (SvelteKit static output).
//
// The admin-web/build/ directory is copied here by build.sh before Go compilation.
// If no admin-web build exists, the placeholder file ensures the embed still compiles
// (the admin-web feature simply won't be available at runtime).
//
// Build flow:
//  1. cd admin-web && npm run build  (produces admin-web/build/)
//  2. build.sh copies admin-web/build/ → internal/adminweb/build/
//  3. go build embeds internal/adminweb/build/ into the binary
package adminweb

import "embed"

// Files contains the pre-built admin web dashboard SPA.
// When the admin-web build is available, this contains the full SPA (index.html, JS, CSS, etc.).
// When the build is not available, this contains only the placeholder file.
//
//go:embed all:build
var Files embed.FS
