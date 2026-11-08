package service

import (
	"regexp"
	"strings"
)

// ImageStudioModelProfile identifies the public model's documented parameter
// contract. Supplier mapping targets do not redefine that contract. Only fixed
// public enums are sent to clients; private targets remain server-side.
type ImageStudioModelProfile struct {
	Provider  string `json:"provider"`
	Profile   string `json:"profile"`
	Transport string `json:"transport,omitempty"`
}

var unknownImageStudioProfile = ImageStudioModelProfile{Provider: "unknown", Profile: "unknown"}

var imageStudioProfilePatterns = []struct {
	provider string
	profile  string
	pattern  *regexp.Regexp
}{
	{"openai", "gpt-image-2.6", regexp.MustCompile(`^gpt-image-2\.6(?:-(?:flare|sunburst))?(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"openai", "gpt-image-2.5", regexp.MustCompile(`^gpt-image-2\.5(?:-(?:flare|sunburst))?(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"openai", "gpt-image-2", regexp.MustCompile(`^gpt-image-2(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"openai", "gpt-image-1.5", regexp.MustCompile(`^gpt-image-1\.5(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"openai", "gpt-image-1-mini", regexp.MustCompile(`^gpt-image-1-mini(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"openai", "gpt-image-1", regexp.MustCompile(`^gpt-image-1(?:-\d{4}-\d{2}-\d{2})?(?:-(?:1k|2k|4k))?$`)},
	{"google", "gemini-2.5-flash-image", regexp.MustCompile(`^gemini-2\.5-flash-image(?:-preview)?(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"google", "gemini-3-pro-image", regexp.MustCompile(`^gemini-3-pro-image(?:-preview)?(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"google", "gemini-3.1-flash-image", regexp.MustCompile(`^gemini-3\.1-flash-image(?:-preview)?(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"google", "gemini-nano-banana-2.1", regexp.MustCompile(`^gemini-nano-banana-2\.1(?:-preview)?(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"google", "gemini-3.1-flash-lite-image", regexp.MustCompile(`^gemini-3\.1-flash-lite-image(?:-preview)?(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"xai", "grok-imagine-image-2.0", regexp.MustCompile(`^grok-imagine-image-2\.0$`)},
	{"xai", "grok-imagine-image-pro", regexp.MustCompile(`^grok-imagine-image-pro(?:-\d{4}-\d{2}-\d{2})?$`)},
	{"xai", "grok-imagine-image", regexp.MustCompile(`^grok-imagine-image(?:-\d{4}-\d{2}-\d{2})?$`)},
}

func imageStudioProfileForModel(model string) ImageStudioModelProfile {
	model = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
	for _, family := range imageStudioProfilePatterns {
		if family.pattern.MatchString(model) {
			return ImageStudioModelProfile{Provider: family.provider, Profile: family.profile}
		}
	}
	return unknownImageStudioProfile
}
