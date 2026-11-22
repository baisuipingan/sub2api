package service

import "slices"

// Agent owns the binding for a custom public ID. Older Workbench bindings
// are only read when Agent has no binding for that ID.
func workbenchModelProfile(id string, agent, legacy map[string]DesktopAgentModelProfileBinding, profiles []DesktopAgentProfile) *DesktopAgentProfile {
	for i := range profiles {
		profile := &profiles[i]
		if profile.ID == id || slices.Contains(profile.Aliases, id) {
			return profile
		}
	}
	binding, explicit := agent[id]
	if !explicit {
		old, ok := legacy[id]
		if ok && !old.Override && old.ProfileID != "" {
			binding = old
		}
	}
	profileID := binding.ProfileID
	if !explicit && profileID == "" && id == "deepseek-v4.1-flash" {
		// OpenCode advertises this exact public ID for DeepSeek V4.1 Flash.
		// https://opencode.ai/docs/console/go/#endpoints
		// DeepSeek's official API identifies the same model as deepseek-flash.
		profileID = "deepseek-flash"
	}
	for i := range profiles {
		if profiles[i].ID == profileID {
			return &profiles[i]
		}
	}
	return nil
}
