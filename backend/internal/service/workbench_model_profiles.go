package service

import "slices"

// Workbench capabilities are declared against the public ID only. Supplier
// credentials, upstream model mappings and desktop-agent settings are never
// consulted. A custom alias may explicitly name an official capability profile.
func workbenchModelProfile(id string, bindings map[string]DesktopAgentModelProfileBinding, profiles []DesktopAgentProfile) *DesktopAgentProfile {
	binding, explicit := bindings[id]
	if explicit && (binding.Override || binding.ProfileID == "") {
		return nil
	}
	for i := range profiles {
		profile := &profiles[i]
		if profile.ID != id && !slices.Contains(profile.Aliases, id) {
			continue
		}
		if explicit && binding.ProfileID != profile.ID {
			return nil
		}
		return profile
	}
	if explicit {
		for i := range profiles {
			if profiles[i].ID == binding.ProfileID {
				return &profiles[i]
			}
		}
	}
	return nil
}

func validWorkbenchModelProfiles(bindings map[string]DesktopAgentModelProfileBinding, profiles []DesktopAgentProfile) bool {
	if len(bindings) > 128 {
		return false
	}
	for id, binding := range bindings {
		if !officeModelID(id) || binding.Override || binding.ProfileID == "" || workbenchModelProfile(id, bindings, profiles) == nil {
			return false
		}
	}
	return true
}
