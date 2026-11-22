package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchDOCXSplitRunsAndPartScope(t *testing.T) {
	ctx := context.Background()
	const name = "报告.docx"
	const namespaces = `xmlns:w="` + workbenchDOCXWordNS + `" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:x="urn:test" mc:Ignorable="x"`
	const body = `<w:document ` + namespaces + `><w:body>
		<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>原公司</w:t></w:r></w:p>
		<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="360"/></w:tabs></w:pPr><w:r><w:t xml:space="default" x:name="xml:space='ignored'">前原</w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>公司后 原公司</w:t></w:r></w:p>
		<w:p><w:r><w:t>原</w:t><w:tab/><w:t>公司</w:t></w:r></w:p>
		<w:p><w:del><w:r><w:delText>原公司</w:delText></w:r></w:del><w:r><w:t>未改</w:t></w:r></w:p>
		</w:body></w:document>`
	original := officeTestZip(t, map[string]string{
		"[Content_Types].xml": `<Types><Override PartName="/word/custom-top.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/></Types>`,
		"word/document.xml":   "\uFEFF" + body,
		"word/custom-top.xml": `<w:hdr ` + namespaces + `><w:p><w:r><w:t>原公</w:t></w:r><w:r><w:t>司</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":    `<w:ftr ` + namespaces + `><w:p><w:r><w:t>原公司</w:t></w:r></w:p></w:ftr>`,
		"docProps/core.xml":   `<cp:coreProperties xmlns:cp="urn:cp" xmlns:d="` + workbenchDOCXTitleNS + `"><d:title>原公司</d:title></cp:coreProperties>`,
		"word/styles.xml":     "unchanged",
	}, "")
	book, err := openWorkbenchDOCX(name, original)
	require.NoError(t, err)
	input := []WorkbenchDOCXEdit{
		{OldText: "原公司", NewText: "甲&乙 ", Scope: "body"},
		{OldText: "甲&乙 ", NewText: "新公司", Scope: "title"},
		{OldText: "原公司", NewText: "页眉", Scope: "header"},
		{OldText: "原公司", NewText: "", Scope: "footer"},
	}
	updated, counts, err := editWorkbenchDOCXPackage(ctx, name, book, input)
	require.NoError(t, err)
	require.Equal(t, []int{3, 1, 1, 1}, counts)
	updatedAgain, _, err := editWorkbenchDOCXPackage(ctx, name, book, input)
	require.NoError(t, err)
	require.Equal(t, updated, updatedAgain, "same operation must have exact same storage hash")

	result, err := openWorkbenchDOCX(name, updated)
	require.NoError(t, err)
	inspection, err := inspectWorkbenchDOCXPackage(ctx, result, WorkbenchDOCXInspectOptions{})
	require.NoError(t, err)
	require.Equal(t, []workbenchDOCXSection{
		{"title", "word/document.xml", "新公司\n"},
		{"body", "word/document.xml", "前甲&乙 后 甲&乙 \n原\t公司\n未改\n"},
		{"title", "docProps/core.xml", "原公司\n"},
		{"header", "word/custom-top.xml", "页眉\n"},
	}, inspection.Sections)

	raw, err := result.Open("word/document.xml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	xmlData, err := io.ReadAll(raw)
	require.NoError(t, err)
	require.Contains(t, string(xmlData), `xml:space="preserve" x:name="xml:space='ignored'">前甲&amp;乙 </w:t>`)
	require.Contains(t, string(xmlData), `<w:rPr><w:b/></w:rPr><w:t xml:space="preserve">后 甲&amp;乙 </w:t>`)
	require.Contains(t, string(xmlData), `mc:Ignorable="x"`)
	require.Contains(t, string(xmlData), `<w:delText>原公司</w:delText>`)
	_, _, err = editWorkbenchDOCXPackage(ctx, name, book, []WorkbenchDOCXEdit{{OldText: "does not occur", NewText: "updated"}})
	require.ErrorContains(t, err, "did not match")
}

func TestWorkbenchDOCXCreateAndPaginate(t *testing.T) {
	ctx := context.Background()
	const name = "项目.docx"
	title := "财务 & 行政"
	body := strings.Repeat("测试🧾", workbenchDOCXPageRunes)
	input := WorkbenchDOCXCreate{Name: name, Title: title, Paragraphs: []string{body, "下一段"}}
	data, err := createWorkbenchDOCXPackage(ctx, input)
	require.NoError(t, err)
	again, err := createWorkbenchDOCXPackage(ctx, input)
	require.NoError(t, err)
	require.Equal(t, data, again)
	book, err := openWorkbenchDOCX(name, data)
	require.NoError(t, err)
	var seen strings.Builder
	options := WorkbenchDOCXInspectOptions{Scope: "body"}
	for i := 0; i < 5; i++ {
		page, err := inspectWorkbenchDOCXPackage(ctx, book, options)
		require.NoError(t, err)
		for _, section := range page.Sections {
			_, _ = seen.WriteString(section.Text)
		}
		if page.NextOffset == nil {
			break
		}
		require.Greater(t, *page.NextOffset, options.Offset)
		options.Offset = *page.NextOffset
	}
	require.Equal(t, title+"\n"+body+"\n下一段\n", seen.String())

	documentTitle, err := inspectWorkbenchDOCXPackage(ctx, book, WorkbenchDOCXInspectOptions{Scope: "title"})
	require.NoError(t, err)
	require.Equal(t, []workbenchDOCXSection{{"title", "word/document.xml", title + "\n"}, {"title", "docProps/core.xml", title + "\n"}}, documentTitle.Sections)
}
