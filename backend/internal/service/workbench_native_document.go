package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	workbenchDOCXWordNS       = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	workbenchDOCXStrictWordNS = "http://purl.oclc.org/ooxml/wordprocessingml/main"
	workbenchDOCXTitleNS      = "http://purl.org/dc/elements/1.1/"
	workbenchDOCXMaxXML       = 128 << 20
	workbenchDOCXPageRunes    = 12000
)

type WorkbenchDOCXEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
	Scope   string `json:"scope,omitempty"`
}

type WorkbenchDOCXCreate struct {
	Name       string   `json:"name"`
	Title      string   `json:"title,omitempty"`
	Paragraphs []string `json:"paragraphs"`
}

type WorkbenchDOCXInspectOptions struct {
	Scope  string `json:"scope,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type WorkbenchDOCXEditResult struct {
	Artifact     *OfficeResource `json:"artifact"`
	Replacements []int           `json:"replacements,omitempty"`
	Verified     bool            `json:"verified"`
}

type workbenchDOCXSection struct {
	Scope string `json:"scope"`
	Part  string `json:"part"`
	Text  string `json:"text"`
}

type workbenchDOCXInspection struct {
	Resource   *OfficeResource        `json:"resource"`
	Sections   []workbenchDOCXSection `json:"sections"`
	NextOffset *int                   `json:"next_offset,omitempty"`
}

func (s *OfficeResourcesService) readWorkbenchDOCX(ctx context.Context, userID int64, versionID string) (*OfficeResource, *zip.Reader, error) {
	resource, err := s.repo.GetResourceVersion(ctx, userID, versionID, true)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(path.Ext(resource.Name), ".docx") || resource.MIME != officeFileMIMEs[".docx"] {
		return nil, nil, fmt.Errorf("%w: this tool requires a DOCX file", ErrOfficeResourceType)
	}
	store, err := s.storage(ctx, resource.Endpoint, resource.Region, resource.Bucket)
	if err != nil {
		return nil, nil, err
	}
	reader, ok := store.(GalleryObjectReader)
	if !ok {
		return nil, nil, ErrOfficeStorageUnavailable
	}
	data, err := reader.Get(ctx, resource.ObjectKey, OfficeResourceMaxBytes)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) != resource.Bytes || officeResourceHash(data) != resource.SHA256 {
		return nil, nil, ErrOfficeConflict
	}
	book, err := openWorkbenchDOCX(resource.Name, data)
	return resource, book, err
}

func openWorkbenchDOCX(name string, data []byte) (*zip.Reader, error) {
	if _, _, err := validateOfficeFile(name, data); err != nil {
		return nil, err
	}
	book, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrOfficeResourceType
	}
	// Check additions before performing them. An archive may advertise
	// impossible ZIP64 sizes even when its compressed payload fits the limit.
	remaining := uint64(workbenchDOCXMaxXML)
	for _, file := range book.File {
		if file.UncompressedSize64 > remaining {
			return nil, ErrOfficeResourceTooLarge
		}
		remaining -= file.UncompressedSize64
	}
	return book, nil
}

func (s *OfficeResourcesService) inspectWorkbenchDOCX(ctx context.Context, userID int64, versionID string, options ...WorkbenchDOCXInspectOptions) (any, error) {
	var option WorkbenchDOCXInspectOptions
	if len(options) > 0 {
		option = options[0]
	}
	if option.Offset < 0 || !workbenchDOCXScope(option.Scope) {
		return nil, ErrOfficeInvalid
	}
	release, err := s.AcquireIO()
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resource, book, err := s.readWorkbenchDOCX(ctx, userID, versionID)
	if err != nil {
		return nil, err
	}
	result, err := inspectWorkbenchDOCXPackage(ctx, book, option)
	if err != nil {
		return nil, err
	}
	result.Resource = resource
	return result, nil
}

func (s *OfficeResourcesService) editWorkbenchDOCX(ctx context.Context, userID int64, versionID, publicationID string, edits []WorkbenchDOCXEdit, leases ...*OfficeRunLease) (any, error) {
	if publicationID == "" || len(publicationID) > 256 || len(edits) == 0 {
		return nil, ErrOfficeInvalid
	}
	for _, edit := range edits {
		if !workbenchDOCXScope(edit.Scope) || edit.OldText == "" || !workbenchDOCXText(edit.OldText) || !workbenchDOCXText(edit.NewText) {
			return nil, fmt.Errorf("%w: DOCX replacements require exact, single-line text and a valid scope", ErrOfficeInvalid)
		}
	}

	release, err := s.AcquireIO()
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	parent, book, err := s.readWorkbenchDOCX(ctx, userID, versionID)
	if err != nil {
		return nil, err
	}
	edited, replacements, err := editWorkbenchDOCXPackage(ctx, parent.Name, book, edits)
	if err != nil {
		return nil, err
	}
	resource := OfficeResource{
		ID: parent.ID, VersionID: uuid.NewString(), UserID: userID, ParentVersionID: parent.VersionID,
		Name: parent.Name, MIME: parent.MIME, Bytes: int64(len(edited)), SHA256: officeResourceHash(edited),
		PreviewKind: "unsupported", Source: "generated", PublicationID: "docx:" + officeResourceHash([]byte(publicationID)),
		PreviewSHA256: officeResourceHash(nil),
	}
	published, err := s.publishWorkbenchDOCX(ctx, resource, edited, leases)
	if err != nil {
		return nil, err
	}
	return &WorkbenchDOCXEditResult{Artifact: published, Replacements: replacements, Verified: true}, nil
}

func (s *OfficeResourcesService) publishWorkbenchDOCX(ctx context.Context, resource OfficeResource, data []byte, leases []*OfficeRunLease) (*OfficeResource, error) {
	var lease *OfficeRunLease
	if len(leases) > 0 {
		lease = leases[0]
	}
	if lease != nil {
		resource.RunID = lease.Run.ID
	}
	published, err := s.persistVersion(ctx, resource, lease, data, nil)
	if err != nil {
		return nil, err
	}
	// Version records without a legacy worker run do not acquire a role from
	// the repository. This receipt is an output of the current native turn.
	published.Role = "output"
	return published, nil
}

func (s *OfficeResourcesService) createWorkbenchDOCX(ctx context.Context, userID int64, publicationID string, input WorkbenchDOCXCreate, leases ...*OfficeRunLease) (any, error) {
	if publicationID == "" || len(publicationID) > 256 {
		return nil, ErrOfficeInvalid
	}
	release, err := s.AcquireIO()
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if input.Name == "" {
		input.Name = "文档.docx"
	} else if path.Ext(input.Name) == "" {
		input.Name += ".docx"
	}
	data, err := createWorkbenchDOCXPackage(ctx, input)
	if err != nil {
		return nil, err
	}
	resource := OfficeResource{
		ID: uuid.NewString(), VersionID: uuid.NewString(), UserID: userID,
		Name: input.Name, MIME: officeFileMIMEs[".docx"], Bytes: int64(len(data)), SHA256: officeResourceHash(data),
		PreviewKind: "unsupported", Source: "generated", PublicationID: "docx:" + officeResourceHash([]byte(publicationID)),
		PreviewSHA256: officeResourceHash(nil),
	}
	published, err := s.publishWorkbenchDOCX(ctx, resource, data, leases)
	if err != nil {
		return nil, err
	}
	return &WorkbenchDOCXEditResult{Artifact: published, Verified: true}, nil
}

func workbenchDOCXScope(scope string) bool {
	return scope == "" || scope == "all" || scope == "body" || scope == "title" || scope == "header" || scope == "footer"
}

func workbenchDOCXText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsAny(value, "\u2028\u2029\ufffe\uffff") && strings.IndexFunc(value, unicode.IsControl) < 0
}

type workbenchDOCXPart struct {
	file  *zip.File
	scope string
}

func workbenchDOCXParts(book *zip.Reader) ([]workbenchDOCXPart, error) {
	// The package content types cover named parts generated by Word as well as
	// documents with custom header and footer filenames.
	contentTypes := make(map[string]string)
	for _, file := range book.File {
		if file.Name != "[Content_Types].xml" {
			continue
		}
		raw, err := readWorkbenchDOCXPart(file)
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Overrides []struct {
				Name string `xml:"PartName,attr"`
				Type string `xml:"ContentType,attr"`
			} `xml:"Override"`
		}
		if xml.Unmarshal(raw, &manifest) != nil {
			return nil, ErrOfficeResourceType
		}
		for _, override := range manifest.Overrides {
			switch override.Type {
			case "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml":
				contentTypes[strings.TrimPrefix(override.Name, "/")] = "header"
			case "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml":
				contentTypes[strings.TrimPrefix(override.Name, "/")] = "footer"
			case "application/vnd.openxmlformats-package.core-properties+xml":
				contentTypes[strings.TrimPrefix(override.Name, "/")] = "title"
			}
		}
		break
	}
	parts := make([]workbenchDOCXPart, 0, 4)
	for _, file := range book.File {
		scope := contentTypes[file.Name]
		switch {
		case file.Name == "word/document.xml":
			scope = "body"
		case file.Name == "docProps/core.xml":
			scope = "title"
		case scope == "" && path.Dir(file.Name) == "word" && path.Ext(file.Name) == ".xml":
			name := path.Base(file.Name)
			for _, candidate := range []string{"header", "footer"} {
				if strings.HasPrefix(name, candidate) {
					scope = candidate
					break
				}
			}
		}
		if scope != "" {
			parts = append(parts, workbenchDOCXPart{file, scope})
		}
	}
	sort.SliceStable(parts, func(i, j int) bool {
		order := []string{"body", "title", "header", "footer"}
		if parts[i].scope != parts[j].scope {
			return slices.Index(order, parts[i].scope) < slices.Index(order, parts[j].scope)
		}
		return parts[i].file.Name < parts[j].file.Name
	})
	return parts, nil
}

func (p workbenchDOCXPart) matches(scope string) bool {
	return scope == "" || scope == "all" || scope == p.scope || (scope == "title" && p.scope == "body")
}

func readWorkbenchDOCXPart(part *zip.File) ([]byte, error) {
	file, err := part.Open()
	if err != nil {
		return nil, ErrOfficeResourceType
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(part.UncompressedSize64)+1))
	if err != nil || uint64(len(raw)) != part.UncompressedSize64 {
		return nil, ErrOfficeResourceType
	}
	return raw, nil
}

type workbenchDOCXTextNode struct {
	start, content, end int
	spaceAttribute      bool
	text, original      string
}

type workbenchDOCXParagraph struct {
	title bool
	nodes []workbenchDOCXTextNode
}

func (p *workbenchDOCXParagraph) text() string {
	var out strings.Builder
	for _, node := range p.nodes {
		out.WriteString(node.text)
	}
	return out.String()
}

func workbenchWordName(name xml.Name, local string) bool {
	return name.Local == local && (name.Space == workbenchDOCXWordNS || name.Space == workbenchDOCXStrictWordNS)
}

// Track raw byte offsets while decoding namespaces and entities. Rewriting
// only changed text nodes preserves the package's original namespace prefixes,
// Word extensions and relationships.
func parseWorkbenchDOCXPart(ctx context.Context, raw []byte, scope string) ([]*workbenchDOCXParagraph, error) {
	bomBytes := 0
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		bomBytes = 3
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw[bomBytes:]))
	var paragraphs []*workbenchDOCXParagraph
	type openParagraph struct {
		depth int
		value *workbenchDOCXParagraph
	}
	var stack []openParagraph
	var runs []int
	var current *workbenchDOCXTextNode
	var currentText strings.Builder
	depth, textDepth, deletedDepth, pPrDepth := 0, 0, 0, 0
	root, rootEnded := false, false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offset := bomBytes + int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrOfficeResourceType
		}
		switch node := token.(type) {
		case xml.Directive:
			return nil, ErrOfficeResourceType
		case xml.StartElement:
			depth++
			if depth == 1 {
				if root {
					return nil, ErrOfficeResourceType
				}
				expected := map[string]string{"body": "document", "header": "hdr", "footer": "ftr"}
				if scope != "title" && !workbenchWordName(node.Name, expected[scope]) {
					return nil, ErrOfficeResourceType
				}
				root = true
			}
			if current != nil {
				return nil, ErrOfficeResourceType
			}
			if deletedDepth != 0 {
				continue
			}
			switch {
			case workbenchWordName(node.Name, "del"), workbenchWordName(node.Name, "moveFrom"):
				deletedDepth = depth
				continue
			case workbenchWordName(node.Name, "p"):
				paragraph := &workbenchDOCXParagraph{}
				paragraphs = append(paragraphs, paragraph)
				stack = append(stack, openParagraph{depth, paragraph})
			case scope == "title" && depth == 2 && node.Name.Space == workbenchDOCXTitleNS && node.Name.Local == "title":
				paragraph := &workbenchDOCXParagraph{title: true}
				paragraphs = append(paragraphs, paragraph)
				stack = append(stack, openParagraph{depth, paragraph})
			}
			if len(stack) == 0 {
				continue
			}
			paragraph := stack[len(stack)-1]
			runContent := len(runs) > 0 && depth == runs[len(runs)-1]+1
			switch {
			case workbenchWordName(node.Name, "r"):
				runs = append(runs, depth)
			case workbenchWordName(node.Name, "pPr") && depth == paragraph.depth+1:
				pPrDepth = depth
			case scope == "body" && workbenchWordName(node.Name, "pStyle") && pPrDepth != 0 && depth == pPrDepth+1:
				for _, attr := range node.Attr {
					if workbenchWordName(attr.Name, "val") {
						paragraph.value.title = strings.EqualFold(attr.Value, "title") || strings.EqualFold(attr.Value, "subtitle")
					}
				}
			case runContent && workbenchWordName(node.Name, "t"), scope == "title" && depth == paragraph.depth:
				current = &workbenchDOCXTextNode{start: offset, content: bomBytes + int(decoder.InputOffset())}
				textDepth = depth
				for _, attr := range node.Attr {
					current.spaceAttribute = current.spaceAttribute || attr.Name == (xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "space"})
				}
			case runContent && workbenchWordName(node.Name, "tab"):
				paragraph.value.nodes = append(paragraph.value.nodes, workbenchDOCXTextNode{text: "\t", original: "\t"})
			case runContent && slices.Contains([]string{"br", "cr", "drawing", "pict", "fldChar", "instrText"}, node.Name.Local) && workbenchWordName(node.Name, node.Name.Local):
				paragraph.value.nodes = append(paragraph.value.nodes, workbenchDOCXTextNode{text: "\n", original: "\n"})
			}
		case xml.CharData:
			if current != nil {
				currentText.Write(node)
			} else if depth == 0 && len(bytes.TrimSpace(node)) != 0 {
				return nil, ErrOfficeResourceType
			}
		case xml.EndElement:
			if current != nil && textDepth == depth {
				current.end = offset
				current.text = currentText.String()
				current.original = current.text
				if current.text != "" {
					paragraph := stack[len(stack)-1].value
					paragraph.nodes = append(paragraph.nodes, *current)
				}
				current = nil
				currentText.Reset()
			}
			if pPrDepth == depth {
				pPrDepth = 0
			}
			if len(runs) > 0 && runs[len(runs)-1] == depth {
				runs = runs[:len(runs)-1]
			}
			if deletedDepth == depth {
				deletedDepth = 0
			}
			if len(stack) > 0 && stack[len(stack)-1].depth == depth {
				stack = stack[:len(stack)-1]
			}
			if depth == 1 {
				rootEnded = true
			}
			depth--
		}
	}
	if !rootEnded || depth != 0 {
		return nil, ErrOfficeResourceType
	}
	return paragraphs, nil
}

func inspectWorkbenchDOCXPackage(ctx context.Context, book *zip.Reader, option WorkbenchDOCXInspectOptions) (*workbenchDOCXInspection, error) {
	parts, err := workbenchDOCXParts(book)
	if err != nil {
		return nil, err
	}
	result := &workbenchDOCXInspection{Sections: []workbenchDOCXSection{}}
	offset := option.Offset
	remaining := workbenchDOCXPageRunes
	for _, part := range parts {
		if !part.matches(option.Scope) {
			continue
		}
		raw, err := readWorkbenchDOCXPart(part.file)
		if err != nil {
			return nil, err
		}
		paragraphs, err := parseWorkbenchDOCXPart(ctx, raw, part.scope)
		if err != nil {
			return nil, err
		}
		var section strings.Builder
		sectionScope := ""
		for _, paragraph := range paragraphs {
			if option.Scope == "title" && !paragraph.title {
				continue
			}
			content := paragraph.text()
			if content == "" {
				continue
			}
			scope := part.scope
			if paragraph.title {
				scope = "title"
			}
			if sectionScope != scope {
				if section.Len() > 0 {
					result.Sections = append(result.Sections, workbenchDOCXSection{sectionScope, part.file.Name, section.String()})
					section.Reset()
				}
				sectionScope = scope
			}
			// Count the end-of-paragraph newline in the cursor, including when
			// paging through a single unusually long paragraph.
			content += "\n"
			runes := utf8.RuneCountInString(content)
			if runes <= offset {
				offset -= runes
				continue
			}
			for offset > 0 {
				_, size := utf8.DecodeRuneInString(content)
				content = content[size:]
				offset--
				runes--
			}
			if runes > remaining {
				end := 0
				for i := 0; i < remaining; i++ {
					_, size := utf8.DecodeRuneInString(content[end:])
					end += size
				}
				section.WriteString(content[:end])
				if section.Len() > 0 {
					result.Sections = append(result.Sections, workbenchDOCXSection{sectionScope, part.file.Name, section.String()})
				}
				next := option.Offset + workbenchDOCXPageRunes
				result.NextOffset = &next
				return result, nil
			}
			section.WriteString(content)
			remaining -= runes
		}
		if section.Len() > 0 {
			result.Sections = append(result.Sections, workbenchDOCXSection{sectionScope, part.file.Name, section.String()})
		}
	}
	if offset > 0 {
		return nil, ErrOfficeInvalid
	}
	return result, nil
}

func (p *workbenchDOCXParagraph) edit(edit WorkbenchDOCXEdit, growthLeft *int64) (int, error) {
	text := p.text()
	count := strings.Count(text, edit.OldText)
	if count == 0 || edit.OldText == edit.NewText {
		return count, nil
	}
	growth := int64(count) * int64(len(edit.NewText)-len(edit.OldText))
	if growth > *growthLeft {
		return 0, ErrOfficeResourceTooLarge
	}
	*growthLeft -= growth

	// Walk the original text and the run boundaries together. The replacement
	// inherits formatting from its first run. Text before and after the match
	// stays in its original runs, including matches split across runs.
	node, nodeStart, written := 0, 0, 0
	var output strings.Builder
	advance := func() {
		current := &p.nodes[node]
		length := len(current.text)
		if written != 0 {
			output.WriteString(current.text[written:])
			current.text = output.String()
			output.Reset()
			written = 0
		}
		nodeStart += length
		node++
	}
	for cursor := 0; cursor < len(text); {
		index := strings.Index(text[cursor:], edit.OldText)
		if index < 0 {
			break
		}
		start := cursor + index
		end := start + len(edit.OldText)
		for nodeStart+len(p.nodes[node].text) <= start {
			advance()
		}
		output.WriteString(p.nodes[node].text[written : start-nodeStart])
		output.WriteString(edit.NewText)
		for nodeStart+len(p.nodes[node].text) < end {
			written = len(p.nodes[node].text)
			advance()
		}
		written = end - nodeStart
		cursor = end
	}
	if written != 0 {
		advance()
	}
	return count, nil
}

var workbenchDOCXXMLAttribute = regexp.MustCompile(`([^\s=/<>]+)\s*=\s*(?:"[^"]*"|'[^']*')`)

func renderWorkbenchDOCXPart(raw []byte, paragraphs []*workbenchDOCXParagraph) ([]byte, error) {
	var changes []workbenchDOCXTextNode
	for _, paragraph := range paragraphs {
		for _, node := range paragraph.nodes {
			if node.text != node.original {
				changes = append(changes, node)
			}
		}
	}
	if len(changes) == 0 {
		return raw, nil
	}
	slices.SortFunc(changes, func(a, b workbenchDOCXTextNode) int { return a.start - b.start })
	var out bytes.Buffer
	offset := 0
	for _, node := range changes {
		out.Write(raw[offset:node.start])
		tag := raw[node.start:node.content]
		if strings.TrimSpace(node.text) != node.text {
			if node.spaceAttribute {
				for _, attribute := range workbenchDOCXXMLAttribute.FindAllSubmatchIndex(tag, -1) {
					if string(tag[attribute[2]:attribute[3]]) == "xml:space" {
						out.Write(tag[:attribute[0]])
						out.WriteString(`xml:space="preserve"`)
						tag = tag[attribute[1]:]
						break
					}
				}
			} else {
				out.Write(tag[:len(tag)-1])
				out.WriteString(` xml:space="preserve"`)
				tag = tag[len(tag)-1:]
			}
		}
		out.Write(tag)
		if err := xml.EscapeText(&out, []byte(node.text)); err != nil {
			return nil, err
		}
		offset = node.end
		if out.Len() > workbenchDOCXMaxXML {
			return nil, ErrOfficeResourceTooLarge
		}
	}
	out.Write(raw[offset:])
	if out.Len() > workbenchDOCXMaxXML {
		return nil, ErrOfficeResourceTooLarge
	}
	return out.Bytes(), nil
}

func editWorkbenchDOCXPackage(ctx context.Context, name string, book *zip.Reader, edits []WorkbenchDOCXEdit) ([]byte, []int, error) {
	parts, err := workbenchDOCXParts(book)
	if err != nil {
		return nil, nil, err
	}
	var rawSize int64
	for _, file := range book.File {
		rawSize += int64(file.UncompressedSize64)
	}
	growthLeft := int64(workbenchDOCXMaxXML) - rawSize
	replacements := make([]int, len(edits))
	changed := make(map[string][]byte)
	for _, part := range parts {
		if !slices.ContainsFunc(edits, func(edit WorkbenchDOCXEdit) bool { return part.matches(edit.Scope) }) {
			continue
		}
		raw, err := readWorkbenchDOCXPart(part.file)
		if err != nil {
			return nil, nil, err
		}
		paragraphs, err := parseWorkbenchDOCXPart(ctx, raw, part.scope)
		if err != nil {
			return nil, nil, err
		}
		partChanged := false
		for i, edit := range edits {
			if !part.matches(edit.Scope) {
				continue
			}
			for _, paragraph := range paragraphs {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
				if edit.Scope == "title" && !paragraph.title {
					continue
				}
				count, err := paragraph.edit(edit, &growthLeft)
				if err != nil {
					return nil, nil, err
				}
				replacements[i] += count
				partChanged = partChanged || (count != 0 && edit.OldText != edit.NewText)
			}
		}
		if !partChanged {
			continue
		}
		output, err := renderWorkbenchDOCXPart(raw, paragraphs)
		if err != nil {
			return nil, nil, err
		}
		if !bytes.Equal(raw, output) {
			changed[part.file.Name] = output
		}
	}
	for i, count := range replacements {
		if count == 0 {
			return nil, nil, fmt.Errorf("%w: DOCX replacement %d did not match any text", ErrOfficeInvalid, i+1)
		}
	}
	if len(changed) == 0 {
		return nil, nil, fmt.Errorf("%w: DOCX replacements did not change the document", ErrOfficeInvalid)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := writer.SetComment(book.Comment); err != nil {
		return nil, nil, ErrOfficeResourceType
	}
	for _, file := range book.File {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		raw, changed := changed[file.Name]
		if !changed {
			if err := writer.Copy(file); err != nil {
				return nil, nil, err
			}
			continue
		}
		header := file.FileHeader
		header.Method = zip.Deflate
		entry, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, nil, err
		}
		if _, err = entry.Write(raw); err != nil {
			return nil, nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, nil, err
	}
	reopened, err := openWorkbenchDOCX(name, output.Bytes())
	if err != nil {
		return nil, nil, err
	}
	for _, part := range parts {
		want, ok := changed[part.file.Name]
		if !ok {
			continue
		}
		got, err := reopened.Open(part.file.Name)
		if err != nil {
			return nil, nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(got, int64(len(want))+1))
		_ = got.Close()
		if err != nil || !bytes.Equal(raw, want) {
			return nil, nil, ErrOfficeConflict
		}
		if _, err = parseWorkbenchDOCXPart(ctx, raw, part.scope); err != nil {
			return nil, nil, err
		}
	}
	return output.Bytes(), replacements, nil
}

func createWorkbenchDOCXPackage(ctx context.Context, input WorkbenchDOCXCreate) ([]byte, error) {
	if !officeSafeName(input.Name) || !strings.EqualFold(path.Ext(input.Name), ".docx") || !workbenchDOCXText(input.Title) {
		return nil, fmt.Errorf("%w: use a DOCX filename and a single-line title", ErrOfficeInvalid)
	}
	if input.Title == "" && len(input.Paragraphs) == 0 {
		return nil, fmt.Errorf("%w: provide the document content", ErrOfficeInvalid)
	}
	var paragraphs strings.Builder
	if input.Title != "" {
		paragraphs.WriteString(`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t xml:space="preserve">`)
		if err := xml.EscapeText(&paragraphs, []byte(input.Title)); err != nil {
			return nil, err
		}
		paragraphs.WriteString(`</w:t></w:r></w:p>`)
	}
	for _, paragraph := range input.Paragraphs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !workbenchDOCXText(paragraph) {
			return nil, fmt.Errorf("%w: put each line of text in a separate paragraph", ErrOfficeInvalid)
		}
		paragraphs.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		if err := xml.EscapeText(&paragraphs, []byte(paragraph)); err != nil {
			return nil, err
		}
		paragraphs.WriteString(`</w:t></w:r></w:p>`)
		if paragraphs.Len() > workbenchDOCXMaxXML {
			return nil, ErrOfficeResourceTooLarge
		}
	}
	core := strings.Builder{}
	core.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="` + workbenchDOCXTitleNS + `"><dc:title>`)
	if err := xml.EscapeText(&core, []byte(input.Title)); err != nil {
		return nil, err
	}
	core.WriteString(`</dc:title></cp:coreProperties>`)
	const officeRelationships = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	const packageRelationships = "http://schemas.openxmlformats.org/package/2006/relationships"
	parts := []struct{ name, contents string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="` + packageRelationships + `"><Relationship Id="rId1" Type="` + officeRelationships + `officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="` + packageRelationships + `/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<Relationships xmlns="` + packageRelationships + `"><Relationship Id="rId1" Type="` + officeRelationships + `styles" Target="styles.xml"/></Relationships>`},
		{"word/styles.xml", `<w:styles xmlns:w="` + workbenchDOCXWordNS + `"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:pPr><w:spacing w:after="160"/></w:pPr><w:rPr><w:sz w:val="22"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="48"/></w:rPr></w:style></w:styles>`},
		{"docProps/core.xml", core.String()},
		{"word/document.xml", `<w:document xmlns:w="` + workbenchDOCXWordNS + `"><w:body>` + paragraphs.String() + `</w:body></w:document>`},
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, part := range parts {
		entry, err := writer.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(entry, part.contents); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	book, err := openWorkbenchDOCX(input.Name, output.Bytes())
	if err != nil {
		return nil, err
	}
	if _, err := inspectWorkbenchDOCXPackage(ctx, book, WorkbenchDOCXInspectOptions{}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
