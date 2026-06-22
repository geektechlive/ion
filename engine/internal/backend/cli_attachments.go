package backend

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dsswift/ion/engine/internal/types"
)

// maxInlineAttachmentBytes caps a file we will base64-inline as a content
// block. Anthropic's PDF limit is ~32MB; staying well under it leaves headroom
// for the ~33% base64 inflation. Files larger than this keep their marker so
// the model falls back to the Read tool.
const maxInlineAttachmentBytes = 24 * 1024 * 1024

// attachmentMarkerRe matches the `[Attached file|image|plan: <path>]` markers
// the harness before_prompt resolver leaves in the prompt after materializing
// the referenced file to an absolute path. Mirrors attachmentResolver.ts
// MARKER_RE.
var attachmentMarkerRe = regexp.MustCompile(`\[Attached (file|image|plan): ([^\]]+)\]`)

// buildCliUserContent turns the resolved prompt plus pre-encoded image
// attachments into the content-block slice for the CLI stream-json user
// message.
//
// PDFs referenced by `[Attached file: <abs>]` markers are read from disk and
// inlined as native `document` blocks so the model ingests them directly,
// instead of the Read tool expanding each page into a separate image block and
// stalling the request for minutes (#789). Images supplied via opts.Attachments
// (already base64) become `image` blocks, mirroring ApiBackend's
// buildUserContentBlocks.
//
// Anything we cannot or should not inline (non-PDF files, `plan` markers,
// oversized or unreadable files) keeps its marker untouched so the existing
// Read-tool path still works. The non-attachment case returns a single text
// block identical to the previous hardcoded behavior.
func buildCliUserContent(prompt string, attachments []types.ImageAttachment) []map[string]interface{} {
	text := prompt
	media := make([]map[string]interface{}, 0, len(attachments)+1)

	// Document blocks: read PDFs referenced by `file` markers from disk.
	seen := make(map[string]bool)
	for _, m := range attachmentMarkerRe.FindAllStringSubmatch(prompt, -1) {
		full, kind, path := m[0], m[1], m[2]
		if kind != "file" || strings.ToLower(filepath.Ext(path)) != ".pdf" {
			continue // only PDFs get the document-block treatment in v1
		}
		if seen[path] {
			text = strings.ReplaceAll(text, full, "")
			continue // already inlined this file; just drop the duplicate marker
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 || info.Size() > maxInlineAttachmentBytes {
			continue // leave marker; model falls back to the Read tool
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		seen[path] = true
		media = append(media, map[string]interface{}{
			"type": "document",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "application/pdf",
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		})
		text = strings.ReplaceAll(text, full, "") // consumed -> strip marker
	}

	// Image blocks: opts.Attachments carries pre-encoded base64 images. The
	// desktop adds a redundant `[Attached image: ...]` marker 1:1 alongside each
	// one, so when we emit image blocks we strip those markers to avoid the
	// model also trying to Read the path.
	for _, a := range attachments {
		if a.Data == "" || a.MediaType == "" {
			continue
		}
		media = append(media, map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": a.MediaType,
				"data":       a.Data,
			},
		})
	}
	if len(attachments) > 0 {
		text = attachmentMarkerRe.ReplaceAllStringFunc(text, func(s string) string {
			if sub := attachmentMarkerRe.FindStringSubmatch(s); sub != nil && sub[1] == "image" {
				return ""
			}
			return s
		})
	}

	// Text block first (matches ApiBackend's buildUserContentBlocks). Collapse
	// whitespace left by stripped markers; keep a non-empty placeholder so the
	// message stays well-formed when the prompt was only an attachment.
	text = strings.TrimSpace(text)
	if text == "" && len(media) > 0 {
		text = "(see attached)"
	}
	blocks := make([]map[string]interface{}, 0, len(media)+1)
	blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
	blocks = append(blocks, media...)
	return blocks
}
