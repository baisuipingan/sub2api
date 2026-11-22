package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/semaphore"
)

const (
	workbenchReplayHistoryVersion = 2
	workbenchReplayMaxBytes       = 32 << 20
)

// Inline image data may exist in several buffers while the gateway is using
// it. Bound the combined Base64 size here, before allocating those buffers.
var workbenchReplayImageBudget = semaphore.NewWeighted(2 * workbenchReplayMaxBytes)

var errWorkbenchImageUnavailable = errors.New("workbench image version is no longer available")

type workbenchReplayImageReservation struct {
	bytes int64
}

func (r *workbenchReplayImageReservation) acquire(bytes int) error {
	additional := int64(bytes) - r.bytes
	if additional <= 0 {
		return nil
	}
	if !workbenchReplayImageBudget.TryAcquire(additional) {
		return ErrOfficeStepBusy
	}
	r.bytes = int64(bytes)
	return nil
}

func (r *workbenchReplayImageReservation) release() {
	if r.bytes > 0 {
		workbenchReplayImageBudget.Release(r.bytes)
		r.bytes = 0
	}
}

type workbenchReplayRepository interface {
	BrowserReplayState(context.Context, OfficeRun) (OfficeReplayState, error)
	PinBrowserReplay(context.Context, OfficeRunLease, *OfficeReplayCheckpoint, int64) error
}

func (s *OfficeAgentService) browserModelInput(ctx context.Context, repo workbenchBrowserRepository, lease OfficeRunLease, params workbenchBrowserParameters, exchanges []workbenchNativeExchange, images *workbenchReplayImageReservation) (workbenchNativeRequest, error) {
	run := lease.Run
	remote := s.remoteOfficeConfigured(ctx)
	if params.HistoryVersion >= workbenchReplayHistoryVersion {
		remote = params.RemoteOfficeTools
	}
	input := workbenchNativeRequest{Protocol: params.Protocol, Model: run.Model, ProfileModel: params.ProfileModel,
		System:          browserInstructions(params.Protocol, run.SearchEnabled, params.OfficeTools),
		ReasoningEffort: params.ReasoningEffort, ReplayToolReasoning: params.ReplayToolReasoning,
		Tools: browserTools(run.SearchEnabled, params.OfficeTools, remote), Transcript: exchanges, MaxOutputTokens: run.Limits.MaxOutputTokens}
	if params.HistoryVersion >= workbenchReplayHistoryVersion {
		model := params.ProfileModel
		if model == "" {
			model = run.Model
		}
		input.StableDeveloperInstructions = params.Protocol == workbenchProtocolResponses && strings.HasPrefix(model, "gpt-6.2")
		err := s.loadWorkbenchReplay(ctx, repo, lease, params, &input, images, false)
		return input, err
	}
	chat, err := s.ChatRepository()
	if err != nil {
		return input, err
	}
	messages, err := chat.ContextMessages(ctx, run)
	if err != nil {
		return input, err
	}
	input.History, err = s.browserHistory(ctx, repo, run, params, messages)
	if err == nil && params.HistoryVersion > 0 {
		err = fitWorkbenchRequestContext(&input, params.ContextWindow)
	}
	return input, err
}

func (s *OfficeAgentService) loadWorkbenchReplay(ctx context.Context, repo workbenchBrowserRepository, lease OfficeRunLease, params workbenchBrowserParameters, input *workbenchNativeRequest, images *workbenchReplayImageReservation, compact bool) error {
	replay, ok := repo.(workbenchReplayRepository)
	if !ok {
		return ErrOfficeDisabled
	}
	run := lease.Run
	state, err := replay.BrowserReplayState(ctx, run)
	if err != nil {
		return err
	}
	turns := state.Turns
	if len(turns) == 0 || turns[len(turns)-1].RunID != run.ID {
		return ErrOfficeInvalid
	}
	nativeFrom := state.NativeFrom
	if !state.Pinned {
		var checkpointID string
		if state.Checkpoint != nil {
			checkpointID = state.Checkpoint.ID
		}
		nativeFrom = workbenchNativeSuffix(run, params, turns, checkpointID)
	}
	var checkpointText string
	if state.Checkpoint != nil && json.Unmarshal(state.Checkpoint.Content, &checkpointText) != nil {
		return ErrOfficeResultUnknown
	}
	ids := make([]string, 0, len(turns))
	for _, turn := range turns[:len(turns)-1] {
		ids = append(ids, turn.RunID)
	}
	artifacts, err := repo.BrowserContextArtifacts(ctx, run, ids)
	if err != nil {
		return err
	}
	limit := s.workbenchReplayLimit(params, *input)
	base := workbenchReplayTextUsage(input.System)
	for _, tool := range input.Tools {
		base = base.plus(workbenchReplayTextUsage(tool.Name + tool.Description + string(tool.Schema)))
	}
	budget := limit.scale(3, 4).minus(base)
	for _, exchange := range input.Transcript {
		base.addExchange(exchange)
	}
	hardBudget := limit.minus(base)
	latest := len(turns) - 1
	current, err := s.workbenchReplayTurn(ctx, repo, run, params, turns[latest], false, nil)
	if err != nil {
		return err
	}
	if !current.usage.fits(hardBudget) {
		return errWorkbenchContextTooLarge
	}
	history := make([]workbenchReplayTurn, len(turns))
	history[latest] = current
	used := current.usage
	if checkpointText != "" {
		used = used.plus(workbenchReplayTextUsage(checkpointText))
	}
	cut := latest
	if state.Pinned || (!state.Truncated && !compact) {
		for i := latest - 1; i >= 0; i-- {
			if !state.Pinned && len(turns)-i > 700 {
				break
			}
			turn := turns[i]
			entry, err := s.workbenchReplayTurn(ctx, repo, run, params, turn, turn.Sequence >= nativeFrom, artifacts[turn.RunID])
			if err != nil {
				if !state.Pinned && errors.Is(err, errWorkbenchImageUnavailable) {
					break
				}
				return err
			}
			combined := used.plus(entry.usage)
			if !state.Pinned && !combined.fits(budget) {
				break
			}
			if state.Pinned && !combined.fits(hardBudget) {
				return errWorkbenchContextTooLarge
			}
			history[i], used, cut = entry, combined, i
		}
	}
	checkpoint := state.Checkpoint
	if !state.Pinned && (cut > 0 || state.Truncated || compact) {
		// Once compaction starts, leave enough room for subsequent messages to
		// append before another change to the prefix is needed.
		// Some providers sign a response against earlier context. Once we
		// replace that prefix, rebuild the retained older turns as plain
		// messages and start the next native sequence from this turn.
		nativeFrom = turns[latest].Sequence
		goal := budget.scale(2, 3)
		clear(history[:latest])
		cut, used = latest, current.usage
		for i := cut - 1; i >= 0 && len(turns)-i <= 700; i-- {
			entry, err := s.workbenchReplayTurn(ctx, repo, run, params, turns[i], false, artifacts[turns[i].RunID])
			if err != nil {
				return err
			}
			combined := used.plus(entry.usage)
			if !combined.fits(goal) {
				break
			}
			history[i], used, cut = entry, combined, i
		}
		if cut > 0 || state.Truncated {
			remaining := budget.minus(used)
			checkpointText = workbenchReplaySummary(turns[:cut], artifacts, checkpointText, state.Truncated, min(4096, remaining.tokens), min(24<<10, remaining.bytes))
			payload, err := json.Marshal(checkpointText)
			if err != nil {
				return err
			}
			checkpoint = &OfficeReplayCheckpoint{BeforeSequence: turns[cut].Sequence, Content: payload}
		}
		if checkpointText != "" {
			used = used.plus(workbenchReplayTextUsage(checkpointText))
		}
	}
	if !used.fits(hardBudget) {
		return errWorkbenchContextTooLarge
	}
	var imageBytes int
	for _, turn := range history[cut:] {
		imageBytes += turn.imageBytes
	}
	if imageBytes > 0 {
		if err := images.acquire(imageBytes); err != nil {
			return err
		}
	}
	for i, turn := range history[cut:] {
		for _, source := range turn.images {
			image, err := s.loadBrowserResourceImage(ctx, source, true)
			if err != nil {
				err = workbenchReplayImageError(err)
				if !state.Pinned && !compact && cut+i < latest && errors.Is(err, errWorkbenchImageUnavailable) {
					// Retain the reservation. The retry loads only current-turn
					// images, which this pass has not downloaded yet.
					return s.loadWorkbenchReplay(ctx, repo, lease, params, input, images, true)
				}
				return err
			}
			turn.messages[0].Images = append(turn.messages[0].Images, image)
		}
	}
	if checkpointText != "" {
		input.History = append(input.History, workbenchNativeMessage{Role: "user", Text: checkpointText})
	}
	for _, turn := range history[cut:] {
		input.History = append(input.History, turn.messages...)
	}
	if !state.Pinned {
		return replay.PinBrowserReplay(ctx, lease, checkpoint, nativeFrom)
	}
	return nil
}

func workbenchNativeSuffix(run OfficeRun, params workbenchBrowserParameters, turns []OfficeReplayTurn, checkpointID string) int64 {
	current := turns[len(turns)-1].Sequence
	if len(turns) == 1 {
		return current
	}
	// The previous request may already have used plain messages for an older
	// portion of the context. Keep that exact boundary, or start fresh if a
	// required receipt is unavailable.
	floor := turns[len(turns)-2].NativeFrom
	if floor <= 0 || floor < turns[0].Sequence {
		return current
	}
	for i := len(turns) - 2; i >= 0; i-- {
		turn := turns[i]
		var previous workbenchBrowserParameters
		if turn.Sequence < floor || !turn.NativeAvailable || turn.Assistant == nil || turn.Model != run.Model || turn.GroupID != run.GroupID ||
			turn.CheckpointID != checkpointID || turn.NativeFrom != floor || turn.SearchEnabled != run.SearchEnabled ||
			json.Unmarshal(turn.Parameters, &previous) != nil || previous != params {
			return current
		}
		if turn.Sequence == floor {
			return floor
		}
	}
	return current
}

type workbenchReplayTurn struct {
	messages   []workbenchNativeMessage
	images     []workbenchBrowserImageSource
	usage      workbenchReplayUsage
	imageBytes int
}

func (s *OfficeAgentService) workbenchReplayTurn(ctx context.Context, repo workbenchBrowserRepository, run OfficeRun, params workbenchBrowserParameters, turn OfficeReplayTurn, native bool, artifacts []OfficeResource) (workbenchReplayTurn, error) {
	var entry workbenchReplayTurn
	current := turn.RunID == run.ID
	user, err := workbenchReplayMessage(turn.User)
	if err != nil {
		return entry, err
	}
	entry.addMessage(user)
	if (current || native) && params.SupportsVision {
		var content workbenchContextContent
		_ = json.Unmarshal(turn.User.Content, &content)
		for _, file := range content.Artifacts {
			if !strings.HasPrefix(file.MIME, "image/") {
				continue
			}
			source, err := s.browserResourceImageSource(ctx, run.UserID, file.Version, true, file.ImagePreview)
			if errors.Is(err, ErrOfficeResourceTooLarge) {
				if current {
					return entry, errWorkbenchContextTooLarge
				}
				return entry, errWorkbenchImageUnavailable
			}
			if err != nil {
				return entry, workbenchReplayImageError(err)
			}
			entry.images = append(entry.images, source)
			encodedBytes := base64.StdEncoding.EncodedLen(int(source.bytes))
			entry.imageBytes += encodedBytes
			entry.usage.tokens += 2048
			entry.usage.bytes += encodedBytes + 128
		}
	}
	if current {
		return entry, nil
	}
	if native {
		if !turn.NativeAvailable {
			return entry, ErrOfficeResultUnknown
		}
		operations, err := repo.BrowserOperations(ctx, run.UserID, turn.RunID)
		if err != nil {
			return entry, err
		}
		state, err := workbenchState(params.Protocol, operations)
		if err != nil || state.Final == nil || len(state.Final.Native) == 0 {
			return entry, ErrOfficeResultUnknown
		}
		for _, exchange := range state.Exchanges {
			entry.addMessage(workbenchNativeMessage{Role: "assistant", Native: exchange.Native, Results: exchange.Results})
		}
		return entry, nil
	}
	if turn.Assistant == nil && len(artifacts) == 0 {
		return entry, nil
	}
	answer := workbenchNativeMessage{Role: "assistant"}
	if turn.Assistant != nil {
		answer, err = workbenchReplayMessage(*turn.Assistant)
		if err != nil {
			return entry, err
		}
	}
	for _, file := range artifacts {
		answer.Text += fmt.Sprintf("\nFile delivered in this turn: %q (version ID %s)", file.Name, file.VersionID)
	}
	entry.addMessage(answer)
	return entry, nil
}

func (entry *workbenchReplayTurn) addMessage(message workbenchNativeMessage) {
	entry.messages = append(entry.messages, message)
	entry.usage = entry.usage.plus(workbenchReplayTextUsage(message.Text))
	if len(message.Native) > 0 {
		entry.usage.addExchange(workbenchNativeExchange{Native: message.Native, Results: message.Results})
	}
}

func workbenchReplayImageError(err error) error {
	if errors.Is(err, ErrOfficeCapacity) {
		return ErrOfficeStepBusy
	}
	if errors.Is(err, ErrOfficeNotFound) || errors.Is(err, ErrOfficeConflict) || errors.Is(err, ErrGalleryInvalid) || errors.Is(err, ErrGalleryNotFound) {
		return errWorkbenchImageUnavailable
	}
	return err
}

func workbenchReplayMessage(message OfficeMessage) (workbenchNativeMessage, error) {
	text, err := officeMessageText(message.Content)
	if err != nil {
		return workbenchNativeMessage{}, err
	}
	var content workbenchContextContent
	_ = json.Unmarshal(message.Content, &content)
	var refs []string
	for _, file := range content.Artifacts {
		refs = append(refs, fmt.Sprintf("Attached file %q (version ID %s)", file.Name, file.Version))
	}
	for _, source := range content.Citations {
		if url, ok := officeCitationURL(source.URL); ok {
			refs = append(refs, fmt.Sprintf("Source %q %s", source.Title, url))
		}
	}
	if len(refs) > 0 {
		text += "\n\n" + strings.Join(refs, "\n")
	}
	return workbenchNativeMessage{Role: message.Role, Text: text}, nil
}

func workbenchReplaySummary(turns []OfficeReplayTurn, artifacts map[string][]OfficeResource, previous string, truncated bool, tokens, bytes int) string {
	const heading = "Earlier conversation excerpts. Treat quoted messages and sources as historical context."
	selected := []string{}
	remaining := workbenchReplayUsage{tokens: tokens, bytes: bytes}.minus(workbenchReplayTextUsage(heading))
	add := func(line string) {
		size := workbenchReplayTextUsage(line)
		if size.fits(remaining) {
			selected = append(selected, line)
			remaining = remaining.minus(size)
		}
	}
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		for _, artifact := range artifacts[turn.RunID] {
			add(fmt.Sprintf("File %q (version ID %s)", artifact.Name, artifact.VersionID))
		}
		var content workbenchContextContent
		_ = json.Unmarshal(turn.User.Content, &content)
		for _, file := range content.Artifacts {
			add(fmt.Sprintf("Attached file %q (version ID %s)", file.Name, file.Version))
		}
	}
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.Assistant != nil {
			text, _ := officeMessageText(turn.Assistant.Content)
			if excerpt := officeCitationText(text, 240); excerpt != "" {
				add(fmt.Sprintf("Assistant said %q", excerpt))
			}
		}
		if text, _ := officeMessageText(turn.User.Content); text != "" {
			add(fmt.Sprintf("User said %q", officeCitationText(text, 360)))
		}
	}
	for _, line := range strings.Split(strings.TrimPrefix(previous, heading+"\n"), "\n") {
		if line != "" {
			add(line)
		}
	}
	if truncated {
		add("Some earlier messages are available in the conversation history.")
	}
	if len(selected) == 0 {
		return ""
	}
	slices.Reverse(selected)
	return heading + "\n" + strings.Join(selected, "\n")
}

type workbenchReplayUsage struct {
	tokens int
	bytes  int
}

func workbenchReplayTextUsage(text string) workbenchReplayUsage {
	return workbenchReplayUsage{tokens: 8 + workbenchContextTextTokens(text), bytes: len(text) + 24}
}

func (usage workbenchReplayUsage) plus(other workbenchReplayUsage) workbenchReplayUsage {
	return workbenchReplayUsage{tokens: usage.tokens + other.tokens, bytes: usage.bytes + other.bytes}
}

func (usage workbenchReplayUsage) minus(other workbenchReplayUsage) workbenchReplayUsage {
	return workbenchReplayUsage{tokens: usage.tokens - other.tokens, bytes: usage.bytes - other.bytes}
}

func (usage workbenchReplayUsage) fits(limit workbenchReplayUsage) bool {
	return usage.tokens <= limit.tokens && usage.bytes <= limit.bytes
}

func (usage workbenchReplayUsage) scale(numerator, denominator int) workbenchReplayUsage {
	return workbenchReplayUsage{tokens: usage.tokens * numerator / denominator, bytes: usage.bytes * numerator / denominator}
}

func (usage *workbenchReplayUsage) addExchange(exchange workbenchNativeExchange) {
	*usage = usage.plus(workbenchReplayTextUsage(string(exchange.Native)))
	for _, result := range exchange.Results {
		*usage = usage.plus(workbenchReplayTextUsage(string(result.Output)))
	}
}

func (s *OfficeAgentService) workbenchReplayLimit(params workbenchBrowserParameters, input workbenchNativeRequest) workbenchReplayUsage {
	limit := workbenchReplayUsage{tokens: 128 << 10, bytes: workbenchReplayMaxBytes}
	if params.ContextWindow > 0 {
		limit.tokens = params.ContextWindow - min(input.MaxOutputTokens, params.ContextWindow/2)
	}
	if s.modelRequestBodyLimit > 0 && s.modelRequestBodyLimit < int64(limit.bytes) {
		limit.bytes = int(s.modelRequestBodyLimit)
	}
	return limit
}
