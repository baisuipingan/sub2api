# Desktop model capability profiles: frontend contract

The settings draft owns both `models` and `model_profiles`. A profile selection updates the model and its binding together; removing a model removes its binding. Public model IDs are matched exactly against builtin IDs and declared aliases. Custom IDs require an explicit profile selection. Upstream physical IDs are not consulted.

The profile endpoint remains `{ profiles: [...] }` and returns reviewed builtin specifications, never saved administrator declarations. A missing, invalid, or failed profile response blocks publishing while preserving the editable draft. Retrying profiles must not reload or discard the draft.

Without `override: true`, capability flags and physical budgets come from the trusted profile. Valid operator defaults (reasoning and per-request output limit), the public ID, display name and default-model choice are preserved. Advanced overrides are explicit. Restoring profile defaults clears the override and restores operator defaults as well.

The interface distinguishes the documented official context window and input limit from the conservative current-client context budget. A `shared_context_budget` profile instead labels its context as shared between input and output and its `max_input_tokens` as a derived conservative input budget, never an independently documented maximum input. The actual current-client budget remains visible separately.

Reviewed notes remain a translated allowlist; unknown codes are not rendered as raw messages or translation keys. In addition to the existing input and reasoning notes, the card explains shared context reservations, conservative output caps, automatic-only reasoning, the need for a Responses-compatible channel or an existing gateway bridge, structured-output compatibility limits, and the Grok 4.5 documentation's xhigh/high equivalence. That equivalence is scoped to its profile, not every Grok model or a global gateway rewrite. Native web search remains disabled because the current unified Responses executor does not encode provider-native search. These notes describe execution boundaries without adding provider-specific forms or changing protocol selection.

The current `none` level omits the reasoning parameter; it does not explicitly disable thinking. An automatic-only profile retains this executable level and a valid default, even if the model can think by default. Official reasoning metadata may be an empty array for models that expose a token budget instead of discrete effort levels; executable `model.reasoning_levels` must still be nonempty and strictly validated. Official model names, custom bindings, advanced overrides and existing draft interactions keep the same ownership and matching rules.

Verification covers profile matching, legacy normalization, unknown models, atomic bindings, override locking/restoration, failed loads and draft-preserving retry, plus existing group/catalog and step-up protections.

Real Chromium acceptance on 2026-11-14 also consumed the complete 37-profile JSON exported by the actual Go registry. The strict frontend parser accepted the mixed sources and empty official effort metadata. Verified DeepSeek's exact low/high/max controls, a custom public alias selecting Claude without changing its identity, automatic official matching across vendors, and Gemini's automatic-only control. Chinese desktop and Chinese/English dark 390px screenshots were inspected with no overflow or raw translation keys. This used a disposable local fixture, no production credentials or paid inference. The fixture entry points and server were removed afterwards; evidence remains under `output/playwright/agent-vendors-*`.

## Verification on 2026-11-14

- 122 affected tests passed: 77 config/profile utility cases, 40 settings component cases, three locale completeness cases and two message-compilation cases. Mixed-vendor fixtures exercise the actual settings card, automatic-only reasoning, shared-context labels, output cap interpolation, trusted note rendering and publishing without changing public model IDs.
- Budget-based models accept empty official effort metadata; malformed official values and empty executable reasoning lists remain rejected. Unknown note codes stay hidden instead of appearing as untranslated text.
- Targeted ESLint and `vue-tsc --noEmit` passed. The complete frontend build (locale gate, `vue-tsc -b` and Vite) passed with existing Browserslist/chunk warnings. Browser acceptance of the combined implementation is recorded above; the subsequent production check is recorded below.

## Production acceptance on 2026-11-14

Release `0.2.21-ds.17` passed the full release CI and was deployed by immutable digest. The authenticated production profile endpoint matched all 37 reviewed registry entries exactly, and the existing two-model configuration remained readable with an unchanged raw setting SHA256. The actual production settings page accepted the mixed-vendor registry without console errors. Adding `deepseek-flash` to an unsaved draft automatically selected its official profile and high default; unsupported efforts were disabled and low/high/max were selectable. Selecting max succeeded. Reloading the saved configuration removed the test draft; no production model settings were saved and no paid inference was requested. Evidence is under `output/deploy/fork-v0.2.21-17/`.

## Verification on 2026-11-13

- 110 affected tests passed: 61 config/profile utility cases, 38 settings component cases, 8 API cases and 3 locale completeness cases. Targeted ESLint completed without warnings. The final combined frontend `vue-tsc -b` and Vite production build passed after the last runtime changes.
- Real Chromium acceptance used the actual Vue settings component, actual Select and stylesheet with local fixture HTTP endpoints. No production credentials or paid inference requests were involved.
- Confirmed official IDs automatically normalize and keep a fixed official profile; custom public IDs show an unverified summary until selection. Profile choice preserves the public identity and persists its binding atomically.
- Confirmed advanced physical fields start locked; enabling overrides makes them editable. Turning overrides off restores the trusted 922,000 context budget while preserving a valid 8,192 output default. Restore defaults resets that output default to 16,384. A save was inspected through the local fixture endpoint.
- Confirmed a simulated profile HTTP 503 disables publishing. Editing the display name and retrying profiles preserves the draft; the subsequent save contains the edited name. The expected 503 was the only console error in that final browser run.
- Captured and inspected Chinese/English, light/dark layouts at 390 and 1,200 pixels. All eight layouts had no horizontal overflow or raw translation keys. Screenshots are in `output/playwright/agent-profiles-*`; the local fixture server and temporary entry files were removed after verification.
