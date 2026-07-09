package whatsapp

import "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"

// buildDocumentPayload builds the inbound-document webhook payload, forwarding
// the document's real MIME type so downstream (SaaS_Construction) consumers do
// not have to guess it from the path — a WhatsApp document can be
// xlsx/docx/csv/…, not just PDF. The path already carries the correct extension.
//
// Fork addition kept in a fork-owned file (not inlined into upstream's
// buildMediaFields) so upstream refactors of that function do not conflict with
// it; the call site there is a single line. See SAAS-INTEGRATION.md.
func buildDocumentPayload(extracted utils.ExtractedMedia) map[string]any {
	doc := map[string]any{"path": extracted.MediaPath}
	if extracted.Caption != "" {
		doc["caption"] = extracted.Caption
	}
	if extracted.MimeType != "" {
		doc["mime_type"] = extracted.MimeType
	}
	return doc
}
