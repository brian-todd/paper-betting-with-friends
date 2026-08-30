// Package assets embeds the templates and static files the server reads at
// runtime, so a deployed binary carries everything it needs.
//
// It lives at the module root because a //go:embed directive can only reach
// files in its own directory or below, and templates/ and static/ sit at the
// root. Moving them under internal/ would buy a tidier package location at the
// cost of churning the Dockerfile, the Makefile and the air config, which is a
// bad trade for a comment.
//
// In development the server reads the same trees from disk instead (see
// cmd/server), so editing a template or a stylesheet still takes effect without
// a rebuild.
package assets

import "embed"

// FS holds the templates/ and static/ trees, rooted so that paths read
// "templates/pages/games.html" and "static/css/style.css" -- the same paths
// they have on disk, which is what lets the disk and embedded sources be
// swapped for one another.
//
//go:embed templates static
var FS embed.FS
