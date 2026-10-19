package apicompat

import "strings"

const (
	reasoningTagUndecided = iota
	reasoningTagContent
	reasoningTagStreaming
)

// leadingReasoningTagFilter separates a leading <thinking>...</thinking> or
// <think>...</think> block emitted in an ordinary assistant text channel.
// DeepSeek V4.1 Flash occasionally puts its internal plan in output_text; the
// Chat Completions contract exposes that text through reasoning_content
// instead, where clients do not render it as part of the answer.
//
// Only a block at the start of the text is treated as reasoning. Literal tag
// text later in a normal answer remains byte-for-byte unchanged.
type leadingReasoningTagFilter struct {
	mode     int
	pending  string
	closeTag string
}

// Feed consumes one output_text delta and returns any newly classified
// reasoning and visible-content text. It buffers the few leading bytes needed
// to decide whether the delta starts a reasoning tag.
func (f *leadingReasoningTagFilter) Feed(delta string) (reasoning, content string) {
	if delta == "" {
		return "", ""
	}
	switch f.mode {
	case reasoningTagContent:
		return "", delta
	case reasoningTagStreaming:
		f.pending += delta
		return f.consumeClosingTag()
	default:
		f.pending += delta
		return f.consumeLeadingTag()
	}
}

// Flush resolves a buffered prefix at end of stream. An unterminated leading
// reasoning block stays hidden from visible content and is returned as
// reasoning; an ordinary prefix is returned as content.
func (f *leadingReasoningTagFilter) Flush() (reasoning, content string) {
	switch f.mode {
	case reasoningTagUndecided:
		text := f.pending
		f.pending = ""
		f.mode = reasoningTagContent
		if reasoning, rest, ok := splitLeadingReasoningTag(text); ok {
			return reasoning, rest
		}
		return "", text
	case reasoningTagStreaming:
		text := f.pending
		f.pending = ""
		f.mode = reasoningTagContent
		return text, ""
	default:
		return "", ""
	}
}

func (f *leadingReasoningTagFilter) consumeLeadingTag() (reasoning, content string) {
	trimmed := strings.TrimLeft(f.pending, " \t\r\n")
	if trimmed == "" {
		return "", ""
	}
	lower := strings.ToLower(trimmed)

	if !strings.HasPrefix(lower, "<think") {
		if len(lower) < len("<think") && strings.HasPrefix("<think", lower) {
			return "", ""
		}
		f.mode = reasoningTagContent
		text := f.pending
		f.pending = ""
		return "", text
	}

	end := strings.IndexByte(lower, '>')
	if end < 0 {
		// A real opening tag is at most "<thinking>". Waiting beyond that can
		// only delay ordinary text that merely begins with the same letters.
		if len(lower) > len("<thinking") {
			f.mode = reasoningTagContent
			text := f.pending
			f.pending = ""
			return "", text
		}
		return "", ""
	}

	openTag := lower[:end+1]
	var tagName string
	switch openTag {
	case "<think>":
		tagName = "think"
	case "<thinking>":
		tagName = "thinking"
	default:
		f.mode = reasoningTagContent
		text := f.pending
		f.pending = ""
		return "", text
	}

	f.mode = reasoningTagStreaming
	f.closeTag = "</" + tagName + ">"
	f.pending = trimmed[end+1:]
	return f.consumeClosingTag()
}

func (f *leadingReasoningTagFilter) consumeClosingTag() (reasoning, content string) {
	lower := strings.ToLower(f.pending)
	closeAt := strings.Index(lower, f.closeTag)
	if closeAt < 0 {
		return "", ""
	}
	reasoning = f.pending[:closeAt]
	content = f.pending[closeAt+len(f.closeTag):]
	f.pending = ""
	f.mode = reasoningTagContent
	// Collapse the structural newline commonly left after </thinking>.
	content = strings.TrimLeft(content, "\r\n")
	return reasoning, content
}

// splitLeadingReasoningTag removes one or more leading thinking-tag blocks
// from a complete assistant message. It reports ok only when at least one
// valid opening tag was found at the start of text. A missing terminator is
// still treated as leaked reasoning and is hidden from visible content.
func splitLeadingReasoningTag(text string) (reasoning, content string, ok bool) {
	rest := text
	var plans []string
	for {
		trimmed := strings.TrimLeft(rest, " \t\r\n")
		openEnd := strings.IndexByte(trimmed, '>')
		if openEnd < 0 {
			break
		}
		openTag := strings.ToLower(trimmed[:openEnd+1])
		var tagName string
		switch openTag {
		case "<think>":
			tagName = "think"
		case "<thinking>":
			tagName = "thinking"
		default:
			return strings.Join(plans, "\n"), rest, ok
		}
		closeTag := "</" + tagName + ">"
		closeAt := strings.Index(strings.ToLower(trimmed), closeTag)
		if closeAt < 0 {
			// A leading, valid open tag with no terminator is still an upstream
			// reasoning-channel glitch. Hide it from visible content.
			plans = append(plans, trimmed[openEnd+1:])
			return strings.Join(plans, "\n"), "", true
		}
		plans = append(plans, trimmed[openEnd+1:closeAt])
		rest = trimmed[closeAt+len(closeTag):]
		ok = true
	}
	return strings.Join(plans, "\n"), strings.TrimLeft(rest, "\r\n"), ok
}
