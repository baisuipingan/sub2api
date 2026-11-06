package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/tidwall/gjson"
)

type openAIImageOutputCounter struct {
	seen         map[string]openAIImageOutput
	seenOrder    []string
	dataOutputs  []openAIImageOutput
	count        int
	maxDataCount int
}

type openAIImageOutput struct {
	key        string
	payloadKey string
	size       string
	sizeRank   uint8
}

func newOpenAIImageOutputCounter() *openAIImageOutputCounter {
	return &openAIImageOutputCounter{
		seen: make(map[string]openAIImageOutput),
	}
}

func (c *openAIImageOutputCounter) Count() int {
	if c == nil {
		return 0
	}
	if c.maxDataCount > c.count {
		return c.maxDataCount
	}
	return c.count
}

func (c *openAIImageOutputCounter) Sizes() []string {
	if c == nil {
		return nil
	}
	sizes := make([]string, 0, c.Count())
	matched := make([]bool, len(c.dataOutputs))
	for _, key := range c.seenOrder {
		output := c.seen[key]
		for index, dataOutput := range c.dataOutputs {
			if !matched[index] && sameOpenAIImageOutput(output, dataOutput) {
				matched[index] = true
				output = mergeOpenAIImageOutputSize(output, dataOutput)
				break
			}
		}
		if output.size != "" {
			sizes = append(sizes, output.size)
		}
	}
	// data and output may repeat images in different orders. Merge only proven
	// identities; preserve the existing max-count rule for any extra slots.
	remaining := c.Count() - len(c.seenOrder)
	for index, output := range c.dataOutputs {
		if remaining == 0 {
			break
		}
		if matched[index] {
			continue
		}
		remaining--
		if output.size != "" {
			sizes = append(sizes, output.size)
		}
	}
	if len(sizes) == 0 {
		return nil
	}
	return sizes
}

func (c *openAIImageOutputCounter) AddJSONResponse(body []byte) {
	if c == nil || len(body) == 0 || !gjson.ValidBytes(body) {
		return
	}
	c.addDataArray(gjson.GetBytes(body, "data"), gjson.GetBytes(body, "size").String())
	c.addOutputArray(gjson.GetBytes(body, "output"), gjson.GetBytes(body, "size").String())
	c.addOutputArray(gjson.GetBytes(body, "response.output"), gjson.GetBytes(body, "response.size").String())
}

func (c *openAIImageOutputCounter) AddSSEData(data []byte) {
	if c == nil || len(data) == 0 || strings.TrimSpace(string(data)) == "[DONE]" || !gjson.ValidBytes(data) {
		return
	}
	root := gjson.ParseBytes(data)
	c.addDataArray(root.Get("data"), root.Get("size").String())
	eventType := strings.TrimSpace(root.Get("type").String())
	switch eventType {
	case "response.output_item.done":
		c.addImageOutputItem(root.Get("item"), root.Get("size").String())
	case "response.completed", "response.done":
		c.addOutputArray(root.Get("response.output"), root.Get("response.size").String())
	case "image_generation.completed", "image_edit.completed":
		if item := root.Get("item"); item.Exists() {
			c.addImageOutputItem(item, root.Get("size").String())
			return
		}
		if output := root.Get("output"); output.Exists() {
			c.addImageOutputItem(output, root.Get("size").String())
			return
		}
		c.addImageOutputItem(root, "")
	}
}

func (c *openAIImageOutputCounter) AddSSEBody(body string) {
	if c == nil || strings.TrimSpace(body) == "" {
		return
	}
	forEachOpenAISSEDataPayload(body, c.AddSSEData)
}

func (c *openAIImageOutputCounter) addDataArray(data gjson.Result, fallbackSize string) {
	if !data.IsArray() {
		return
	}
	items := data.Array()
	outputs := make([]openAIImageOutput, 0, len(items))
	matched := make([]bool, len(c.dataOutputs))
	for _, item := range items {
		if !item.IsObject() {
			continue
		}
		hasImageOutput := strings.TrimSpace(item.Get("url").String()) != "" ||
			strings.TrimSpace(item.Get("b64_json").String()) != ""
		if !hasImageOutput {
			continue
		}
		output := newOpenAIImageOutput(item, fallbackSize)
		for index, previous := range c.dataOutputs {
			if !matched[index] && sameOpenAIImageOutput(previous, output) {
				matched[index] = true
				output = mergeOpenAIImageOutputSize(output, previous)
				c.dataOutputs[index] = output
				break
			}
		}
		outputs = append(outputs, output)
	}
	if len(outputs) >= len(c.dataOutputs) {
		c.dataOutputs = outputs
	}
	if len(outputs) > c.maxDataCount {
		c.maxDataCount = len(outputs)
	}
}

// The image bytes are authoritative. Fall back to per-image metadata, then to
// response-level metadata only when that size is a recognized billing tier.
// detectOpenAIImageResultSize decodes at most a bounded prefix of the image.
func openAIImageOutputSize(item gjson.Result, fallbackSize string) (string, uint8) {
	for _, field := range []string{"b64_json", "result"} {
		encoded := item.Get(field).String()
		if encoded == "" {
			continue
		}
		if actual := detectOpenAIImageResultSize(encoded); actual != "" {
			return actual, 3
		}
	}
	for index, size := range []string{item.Get("size").String(), fallbackSize} {
		size = strings.TrimSpace(size)
		if _, ok := ClassifyImageBillingTier(size); ok {
			return size, uint8(2 - index)
		}
	}
	return "", 0
}

func (c *openAIImageOutputCounter) addOutputArray(output gjson.Result, fallbackSize string) {
	if !output.IsArray() {
		return
	}
	output.ForEach(func(_, item gjson.Result) bool {
		c.addImageOutputItem(item, fallbackSize)
		return true
	})
}

func (c *openAIImageOutputCounter) addImageOutputItem(item gjson.Result, fallbackSize string) {
	if !item.Exists() || !item.IsObject() {
		return
	}
	itemType := strings.TrimSpace(item.Get("type").String())
	if itemType != "" && itemType != "image_generation_call" && itemType != "image_generation.completed" && itemType != "image_edit.completed" {
		return
	}
	if item.Get("partial_image_b64").Exists() {
		return
	}
	output := newOpenAIImageOutput(item, fallbackSize)
	if output.key == "" {
		return
	}
	if previous, exists := c.seen[output.key]; exists {
		c.seen[output.key] = mergeOpenAIImageOutputSize(previous, output)
		return
	}
	c.seen[output.key] = output
	c.seenOrder = append(c.seenOrder, output.key)
	c.count++
}

func newOpenAIImageOutput(item gjson.Result, fallbackSize string) openAIImageOutput {
	result := strings.TrimSpace(item.Get("result").String())
	if result == "" {
		result = strings.TrimSpace(item.Get("b64_json").String())
	}
	if result == "" {
		result = strings.TrimSpace(item.Get("url").String())
	}
	if result == "" {
		return openAIImageOutput{}
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	payloadKey := hashOpenAIImageOutputResult(result)
	if key == "" {
		key = "payload:" + payloadKey
	} else {
		key = "id:" + key
	}
	size, rank := openAIImageOutputSize(item, fallbackSize)
	return openAIImageOutput{key: key, payloadKey: payloadKey, size: size, sizeRank: rank}
}

func sameOpenAIImageOutput(a, b openAIImageOutput) bool {
	if a.key == b.key {
		return true
	}
	if strings.HasPrefix(a.key, "id:") && strings.HasPrefix(b.key, "id:") {
		return false
	}
	return a.payloadKey != "" && a.payloadKey == b.payloadKey
}

func mergeOpenAIImageOutputSize(output, candidate openAIImageOutput) openAIImageOutput {
	if candidate.sizeRank > output.sizeRank {
		output.size, output.sizeRank = candidate.size, candidate.sizeRank
	}
	return output
}

func hashOpenAIImageOutputResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(result))
	return hex.EncodeToString(sum[:])
}

func countOpenAIResponseImageOutputsFromJSONBytes(body []byte) int {
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse(body)
	return counter.Count()
}

func collectOpenAIResponseImageOutputSizesFromJSONBytes(body []byte) []string {
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse(body)
	return counter.Sizes()
}

func countOpenAIImageOutputsFromSSEBody(body string) int {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEBody(body)
	return counter.Count()
}

func collectOpenAIImageOutputSizesFromSSEBody(body string) []string {
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEBody(body)
	return counter.Sizes()
}
