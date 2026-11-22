package service

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

func workbenchTextTool(name string) bool {
	return name == "read_text" || name == "write_text" || name == "edit_text"
}

func workbenchTextExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md", ".csv", ".json", ".py", ".drawio":
		return true
	}
	return false
}

func (s *OfficeAgentService) runWorkbenchTextTool(ctx context.Context, repo workbenchBrowserRepository, lease OfficeRunLease, id string, call workbenchNativeCall) (any, error) {
	var args struct {
		VersionID string              `json:"version_id"`
		Name      string              `json:"name"`
		Text      string              `json:"text"`
		Offset    int                 `json:"offset"`
		Edits     []WorkbenchDOCXEdit `json:"edits"`
	}
	if json.Unmarshal(call.Arguments, &args) != nil || args.Offset < 0 {
		return nil, ErrOfficeInvalid
	}
	if call.Name == "write_text" {
		if !workbenchTextExtension(args.Name) {
			return nil, ErrOfficeResourceType
		}
		artifact, err := s.publishBrowserFile(ctx, lease, id, nil, args.Name, []byte(args.Text))
		return map[string]any{"artifact": artifact, "verified": err == nil}, err
	}
	source, err := s.workbenchInput(ctx, repo, lease.Run, args.VersionID)
	if err != nil {
		return nil, err
	}
	if !workbenchTextExtension(source.Name) {
		return nil, ErrOfficeResourceType
	}
	store, err := s.resources.storage(ctx, source.Endpoint, source.Region, source.Bucket)
	if err != nil {
		return nil, err
	}
	reader, ok := store.(GalleryObjectReader)
	if !ok {
		return nil, ErrOfficeStorageUnavailable
	}
	release, err := s.resources.AcquireIO()
	if err != nil {
		return nil, err
	}
	data, err := reader.Get(ctx, source.ObjectKey, OfficeResourceMaxBytes)
	release()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != source.Bytes || officeResourceHash(data) != source.SHA256 || !utf8.Valid(data) {
		return nil, ErrOfficeConflict
	}
	text := string(data)
	if call.Name == "read_text" {
		// Offset is a byte boundary returned by this tool.
		start := args.Offset
		if start > len(text) || start < len(text) && !utf8.RuneStart(text[start]) {
			return nil, ErrOfficeInvalid
		}
		end := min(len(text), start+48<<10)
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		result := map[string]any{"resource": source, "text": text[start:end]}
		if end < len(text) {
			result["next_offset"] = end
		}
		return result, nil
	}
	if len(args.Edits) == 0 {
		return nil, ErrOfficeInvalid
	}
	replacements := make([]int, len(args.Edits))
	for i, edit := range args.Edits {
		if edit.OldText == "" || edit.OldText == edit.NewText {
			return nil, ErrOfficeInvalid
		}
		count := strings.Count(text, edit.OldText)
		if count == 0 {
			return nil, ErrOfficeInvalid
		}
		if growth := len(edit.NewText) - len(edit.OldText); growth > 0 && int64(count) > (OfficeResourceMaxBytes-int64(len(text)))/int64(growth) {
			return nil, ErrOfficeResourceTooLarge
		}
		replacements[i] = count
		text = strings.ReplaceAll(text, edit.OldText, edit.NewText)
	}
	artifact, err := s.publishBrowserFile(ctx, lease, id, source, source.Name, []byte(text))
	return map[string]any{"artifact": artifact, "verified": err == nil, "replacements": replacements}, err
}

// Draw.io source is rendered as text in the workbench. Native generated files
// use uncompressed XML and reserve the browser editor for an explicit action.
func validWorkbenchDrawio(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	root := ""
	pages := 0
	graphs := 0
	cellIDs := map[string]bool{}
	var references []string
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.Directive:
			return false
		case xml.StartElement:
			if depth == 0 {
				if root != "" || value.Name.Local != "mxfile" && value.Name.Local != "mxGraphModel" {
					return false
				}
				root = value.Name.Local
			}
			depth++
			if depth > 128 {
				return false
			}
			switch value.Name.Local {
			case "diagram":
				pages++
			case "mxGraphModel":
				graphs++
				cellIDs = map[string]bool{}
				references = nil
			case "mxCell", "object", "UserObject":
				for _, attr := range value.Attr {
					if attr.Name.Local == "id" {
						if attr.Value == "" || cellIDs[attr.Value] {
							return false
						}
						cellIDs[attr.Value] = true
					}
					if attr.Name.Local == "source" || attr.Name.Local == "target" || attr.Name.Local == "parent" {
						references = append(references, attr.Value)
					}
				}
			}
		case xml.EndElement:
			depth--
			if value.Name.Local == "mxGraphModel" {
				for _, ref := range references {
					if ref == "" || !cellIDs[ref] {
						return false
					}
				}
				if len(cellIDs) == 0 {
					return false
				}
			}
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(value)) > 0 {
				return false
			}
		}
	}
	return depth == 0 && graphs > 0 && (root == "mxGraphModel" && graphs == 1 || root == "mxfile" && pages == graphs)
}
