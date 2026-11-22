package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type browserContextArtifactRepo struct {
	workbenchBrowserRepository
	runIDs []string
}

func (r *browserContextArtifactRepo) BrowserContextArtifacts(_ context.Context, _ OfficeRun, runIDs []string) (map[string][]OfficeResource, error) {
	r.runIDs = runIDs
	return map[string][]OfficeResource{
		"old-run": {{Name: "Revision.docx", VersionID: "generated-version"}},
	}, nil
}

func TestWorkbenchHistoryCarriesConfirmedTextAndReferences(t *testing.T) {
	repo := &browserContextArtifactRepo{}
	svc := &OfficeAgentService{}
	messages := []OfficeMessage{
		{RunID: "old-run", Role: "user", Content: json.RawMessage(`{"text":"Change the project name.","artifacts":[{"name":"Original.docx","version_id":"original-version"}]}`)},
		{RunID: "old-run", Role: "assistant", Content: json.RawMessage(`{"text":"Revision complete.","reasoning_summary":"Internal summary","citations":[{"title":"Project directory","url":"https://example.com/project","excerpt":"The current project is Atlas."}]}`)},
		{RunID: "current-run", Role: "user", Content: json.RawMessage(`"Continue and verify it."`)},
	}
	history, err := svc.browserHistory(context.Background(), repo, OfficeRun{ID: "current-run"}, workbenchBrowserParameters{HistoryVersion: 1}, messages)
	require.NoError(t, err)
	require.Equal(t, []string{"old-run"}, repo.runIDs)
	input := workbenchNativeRequest{History: history}
	require.NoError(t, fitWorkbenchRequestContext(&input, 10000))
	require.Len(t, input.History, 3)
	require.Contains(t, input.History[0].Text, "original-version")
	require.Contains(t, input.History[0].Text, "generated-version")
	require.Contains(t, input.History[1].Text, "Revision complete.")
	require.Contains(t, input.History[1].Text, "https://example.com/project")
	require.NotContains(t, input.History[1].Text, "Internal summary")
	require.Equal(t, "Continue and verify it.", input.History[2].Text)
}

func TestWorkbenchHistoryCompactionKeepsATurnPrefixAndNativeToolExchange(t *testing.T) {
	history := []workbenchNativeMessage{
		{Role: "user", Text: "The client's legal name is Atlas. " + strings.Repeat("long original request ", 700)},
		{Role: "assistant", Text: "I saved the revision.", ContextReferences: []string{`Delivered file "Revision.docx" (version ID rendered-v1)`}},
		{Role: "user", Text: "Which header was changed?"},
		{Role: "assistant", Text: "The header on page 1."},
		{Role: "user", Text: "Read the revised file."},
	}
	input := workbenchNativeRequest{
		Protocol: workbenchProtocolGemini, Model: "gemini", System: "Use the tool when necessary.", MaxOutputTokens: 200, History: history,
	}
	require.NoError(t, fitWorkbenchRequestContext(&input, 3000))
	require.Len(t, input.History, 4)
	require.Contains(t, input.History[0].Text, "The client's legal name is Atlas.")
	require.Contains(t, input.History[0].Text, "rendered-v1")
	require.Equal(t, history[2:], input.History[1:])
	require.NotContains(t, input.History[1].Text, "Earlier conversation")
	followup := workbenchNativeRequest{
		Protocol: input.Protocol, Model: input.Model, System: input.System, MaxOutputTokens: input.MaxOutputTokens, History: history,
		Transcript: []workbenchNativeExchange{{
			Native:  json.RawMessage(`{"role":"model","parts":[{"functionCall":{"id":"read-file","name":"read_docx","args":{}},"thoughtSignature":"signed"}]}`),
			Results: []workbenchNativeToolResult{{ID: "read-file", Name: "read_docx", Output: json.RawMessage(`{"text":"document content"}`)}},
		}},
	}
	require.NoError(t, fitWorkbenchRequestContext(&followup, 3000))
	require.Equal(t, input.History, followup.History)
	_, body, err := buildWorkbenchNativeRequest(followup)
	require.NoError(t, err)
	require.Contains(t, string(body), `"thoughtSignature":"signed"`)
	require.Contains(t, string(body), `"functionResponse":{"id":"read-file"`)
	followup.Transcript[0].Results[0].Output, err = json.Marshal(strings.Repeat("大", 3000))
	require.NoError(t, err)
	followup.History = history
	require.ErrorIs(t, fitWorkbenchRequestContext(&followup, 3000), errWorkbenchContextTooLarge)
}

func TestWorkbenchHistoryCompactionKeepsFileReferenceWhenTextDoesNotFit(t *testing.T) {
	const reference = `File delivered in this turn: "Revision.docx" (version ID 3bdd47c8-b011-43b6-b089-62312e786658)`
	input := workbenchNativeRequest{
		History: []workbenchNativeMessage{
			{Role: "user", Text: strings.Repeat("很长的历史消息", 800), ContextReferences: []string{reference}},
			{Role: "user", Text: "继续编辑这个文件。"},
		},
		MaxOutputTokens: 100,
	}
	require.NoError(t, fitWorkbenchRequestContext(&input, 1000))
	require.Len(t, input.History, 2)
	require.Contains(t, input.History[0].Text, reference)
	require.NotContains(t, input.History[0].Text, "很长的历史消息")
	require.Equal(t, "继续编辑这个文件。", input.History[1].Text)
}
