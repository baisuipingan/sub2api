package service

import (
	"encoding/json"
	"path"
	"strings"
)

func workbenchOfficeExtension(name string, remote bool) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".docx", ".txt", ".md", ".csv", ".json", ".py", ".drawio":
		return true
	case ".xlsx", ".pptx", ".zip":
		return remote
	default:
		return false
	}
}

func workbenchRemoteTool(name string) bool {
	switch name {
	case "recalc_xlsx", "create_docx", "read_docx", "edit_docx", "batch_edit_docx",
		"create_xlsx", "read_xlsx", "edit_xlsx", "create_pptx", "template_pptx", "read_pptx", "edit_pptx":
		return true
	}
	return false
}

func workbenchExtraTools(remote bool) []workbenchNativeTool {
	tools := []workbenchNativeTool{
		{"read_text", "Read a Markdown, TXT, JSON, CSV, Python source or Draw.io XML file in this conversation. Use next_offset to continue.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"offset":{"type":"integer"}},"required":["version_id"],"additionalProperties":false}`)},
		{"write_text", "Create a downloadable .md, .txt, .json, .csv, .py or .drawio file. For .drawio provide native uncompressed diagrams.net XML with cell IDs and references.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"text":{"type":"string"}},"required":["name","text"],"additionalProperties":false}`)},
		{"edit_text", "Replace exact text in a source file from this conversation and deliver a new version. Supports Markdown, TXT, JSON, CSV, Python source and Draw.io XML.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old_text":{"type":"string"},"new_text":{"type":"string"}},"required":["old_text","new_text"]}}},"required":["version_id","edits"],"additionalProperties":false}`)},
	}
	if !remote {
		return tools
	}
	return append(tools,
		workbenchNativeTool{"batch_edit_docx", "Replace exact text in DOCX files inside a ZIP. Preserves paths and returns a ZIP and per-file results.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"name":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old_text":{"type":"string"},"new_text":{"type":"string"},"scope":{"type":"string","enum":["all","body","title","header","footer"]}},"required":["old_text","new_text"]}}},"required":["version_id","edits"],"additionalProperties":false}`)},
		workbenchNativeTool{"create_xlsx", "Create a downloadable Excel file. A row is an array of string, numeric, boolean or null cells. Strings beginning with = are formulas.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"sheets":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"rows":{"type":"array","items":{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"}]}}}},"required":["name","rows"]}}},"required":["name","sheets"],"additionalProperties":false}`)},
		workbenchNativeTool{"recalc_xlsx", "Recalculate Excel formulas, verify calculated values and return a new file version. Use only when calculated results are needed.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"}},"required":["version_id"],"additionalProperties":false}`)},
		workbenchNativeTool{"read_xlsx", "Read worksheet names and a cell range, including formulas. Use a small range such as A1:F25 for large sheets.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"sheet":{"type":"string"},"range":{"type":"string"},"data_only":{"type":"boolean"}},"required":["version_id"],"additionalProperties":false}`)},
		workbenchNativeTool{"edit_xlsx", "Set cell or rectangular range values, formulas and optional formatting. Provide cells or ranges. A range uses A1 notation, for example B2:D4. Formula strings start with =.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"cells":{"type":"array","items":{"type":"object","properties":{"sheet":{"type":"string"},"cell":{"type":"string"},"value":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"}]},"format":{"type":"string"},"bold":{"type":"boolean"}},"required":["sheet","cell"]}},"ranges":{"type":"array","items":{"type":"object","properties":{"sheet":{"type":"string"},"range":{"type":"string"},"values":{"type":"array","items":{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"}]}}},"format":{"type":"string"},"bold":{"type":"boolean"}},"required":["sheet","range"]}}},"required":["version_id"],"additionalProperties":false}`)},
		workbenchNativeTool{"create_pptx", "Create a PowerPoint file with title and text per slide.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"slides":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"text":{"type":"string"}},"required":["title"]}}},"required":["name","slides"],"additionalProperties":false}`)},
		workbenchNativeTool{"template_pptx", "Create a new PowerPoint file using an attached presentation's slide layouts and theme. Obtain zero-based layout indices with read_pptx. Use keep_template_slides to retain sample slides.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"name":{"type":"string"},"slides":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"text":{"type":"string"},"layout":{"type":"integer"}},"required":["title"]}},"keep_template_slides":{"type":"boolean"}},"required":["version_id","name","slides"],"additionalProperties":false}`)},
		workbenchNativeTool{"read_pptx", "Read slide text, picture shape indices, and available slide layout indices.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"offset":{"type":"integer"}},"required":["version_id"],"additionalProperties":false}`)},
		workbenchNativeTool{"edit_pptx", "Replace exact text or an existing picture, or add or remove slides. Slides and picture shapes are one-based. Reference a PNG or JPEG from this conversation by image_version_id.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"replacements":{"type":"array","items":{"type":"object","properties":{"old_text":{"type":"string"},"new_text":{"type":"string"},"slide":{"type":"integer"}},"required":["old_text","new_text"]}},"replace_images":{"type":"array","items":{"type":"object","properties":{"slide":{"type":"integer"},"shape":{"type":"integer"},"image_version_id":{"type":"string"}},"required":["slide","shape","image_version_id"]}},"add_slides":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"text":{"type":"string"},"layout":{"type":"integer"}},"required":["title"]}},"remove_slides":{"type":"array","items":{"type":"integer"}}},"required":["version_id"],"additionalProperties":false}`)},
	)
}
