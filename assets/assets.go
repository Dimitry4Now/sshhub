// Package assets embeds the trivia question bank and meme gallery.
package assets

import "embed"

//go:embed questions.json memes
var FS embed.FS
