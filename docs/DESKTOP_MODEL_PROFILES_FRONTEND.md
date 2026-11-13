# Desktop model capability profiles: frontend contract

The settings draft owns both `models` and `model_profiles`. A profile selection updates the model and its binding together; removing a model removes its binding. Public model IDs are matched exactly against builtin IDs and declared aliases. Custom IDs require an explicit profile selection. Upstream physical IDs are not consulted.

The profile endpoint remains `{ profiles: [...] }` and returns reviewed builtin specifications, never saved administrator declarations. A missing, invalid, or failed profile response blocks publishing while preserving the editable draft. Retrying profiles must not reload or discard the draft.

Without `override: true`, capability flags and physical budgets come from the trusted profile. Valid operator defaults (reasoning and per-request output limit), the public ID, display name and default-model choice are preserved. Advanced overrides are explicit. Restoring profile defaults clears the override and restores operator defaults as well.

The interface distinguishes the documented official context window and input limit from the conservative current-client context budget. Native OpenAI web search remains unavailable in the current ZCode executor even when the official model supports it. The current `none` level omits the reasoning parameter; it does not explicitly disable reasoning. These execution limitations are shown with translated, stable note codes from the profile registry.

Verification covers profile matching, legacy normalization, unknown models, atomic bindings, override locking/restoration, failed loads and draft-preserving retry, plus existing group/catalog and step-up protections.

## Verification on 2026-11-13

- 110 affected tests passed: 61 config/profile utility cases, 38 settings component cases, 8 API cases and 3 locale completeness cases. Targeted ESLint completed without warnings. The final combined frontend `vue-tsc -b` and Vite production build passed after the last runtime changes.
- Real Chromium acceptance used the actual Vue settings component, actual Select and stylesheet with local fixture HTTP endpoints. No production credentials or paid inference requests were involved.
- Confirmed official IDs automatically normalize and keep a fixed official profile; custom public IDs show an unverified summary until selection. Profile choice preserves the public identity and persists its binding atomically.
- Confirmed advanced physical fields start locked; enabling overrides makes them editable. Turning overrides off restores the trusted 922,000 context budget while preserving a valid 8,192 output default. Restore defaults resets that output default to 16,384. A save was inspected through the local fixture endpoint.
- Confirmed a simulated profile HTTP 503 disables publishing. Editing the display name and retrying profiles preserves the draft; the subsequent save contains the edited name. The expected 503 was the only console error in that final browser run.
- Captured and inspected Chinese/English, light/dark layouts at 390 and 1,200 pixels. All eight layouts had no horizontal overflow or raw translation keys. Screenshots are in `output/playwright/agent-profiles-*`; the local fixture server and temporary entry files were removed after verification.
