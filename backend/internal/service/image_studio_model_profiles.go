package service

import (
	"regexp"
	"sort"
	"strings"
)

// ImageStudioModelProfile identifies a documented parameter family, not the
// supplier account or the transport protocol. Only fixed public enums are sent
// to clients; an unknown private mapping must never become an exposed model ID.
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

func imageStudioAccountProfile(account *Account, upstream string) ImageStudioModelProfile {
	profile := imageStudioProfileForModel(upstream)
	// The subscription bridge is a transport constraint, not another model
	// family. Clients can retain the known family while limiting its parameters
	// to the subset this existing Images bridge actually forwards.
	if account.IsOpenAIOAuthLike() {
		profile.Transport = "bridge"
	}
	return profile
}

func sortedImageStudioProfiles(profiles map[ImageStudioModelProfile]struct{}) []ImageStudioModelProfile {
	result := make([]ImageStudioModelProfile, 0, len(profiles))
	for profile := range profiles {
		result = append(result, profile)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		if result[i].Profile != result[j].Profile {
			return result[i].Profile < result[j].Profile
		}
		return result[i].Transport < result[j].Transport
	})
	return result
}
