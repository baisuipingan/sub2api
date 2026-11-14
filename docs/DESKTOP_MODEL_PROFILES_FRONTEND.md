# Desktop model profile frontend contract

This contract describes the 2026-11-14 revision 2026-11-14.2, deployed in 0.2.21-ds.18. The backend catalogue is the source of reviewed facts. Saved administrator capability values are not an official source. Full release CI and production acceptance passed; see the release record in DESKTOP_MODEL_PROFILES.md and output/deploy/fork-v0.2.21-18/.

## Draft and identity ownership

The settings draft owns models and model_profiles together. Selecting a profile updates its binding and model atomically; removing a model removes its binding. Exact official IDs and explicitly documented aliases auto-resolve. Custom public IDs require an explicit profile choice. No prefix or upstream-private-name inference is performed, and profile choice does not change the public routing ID, display name, or default-model identity.

Without override:true, capabilities and token limits come from the trusted profile. Valid saved reasoning and positive per-request output defaults are preserved. The project baseline of 16,384 is an operational request budget, not an official model maximum or a mandatory reset of existing valid defaults. Advanced overrides are explicit. Disabling an override restores reviewed capabilities while retaining valid defaults; restoring profile defaults also resets the operational defaults.

A missing, invalid, or failed profile response blocks publishing and keeps the editable draft. Retrying profiles must not reload saved settings or discard edits.

## Official token values and presentation

The profile endpoint remains { profiles: [...] }, but now returns the current accepted merged catalogue rather than only compiled-in data. Every displayed official number is separate from the current operational request budget.

| Field | Display and validation |
| --- | --- |
| official_context_window | Positive published shared total, or null when not separately published |
| max_input_tokens | Positive independently published input maximum, or null |
| official_max_output_tokens | Positive published maximum, or null for unpublished / no independent limit |
| model.context_window | Exact official shared total, otherwise the independent input limit |
| model.max_output_tokens | Positive published maximum for fixed limits; 0 only with unlimited / not_published |
| model.default_max_output_tokens | Positive administrator request budget |

No field is calculated as context minus output. Unknown values display “未单独公布” / “Not separately published,” never 0 Token. A model with only an independent input limit labels its running value “最大输入” rather than “shared context.”

The official card presents three labelled facts: total context, independent input maximum, and maximum output. The summary and advanced view show the same semantics. unlimited displays that there is no independent text-output ceiling; not_published displays the absence of a published limit. Neither exposes the wire sentinel 0 as a physical maximum.

The output-limit kinds accepted by the final contract are omitted/empty/fixed, unlimited, and not_published. Fixed limits are positive and may equal context. Unlimited/unpublished kinds require zero max_output_tokens and a positive operational budget no greater than context. The normal default-budget control uses the fixed output maximum when known and context otherwise. Advanced editing of a previously unknown maximum clears the unknown marker and becomes an explicit custom fixed override.

The optional handling of official_max_output_tokens permits older reviewed profile responses to display their existing model limit; the new schema-1 feed explicitly includes this field, using null when appropriate. Malformed non-null official limits, contradictory model limits, invalid capability flags, and empty executable reasoning lists are rejected.

## Conditions and execution limits

Reviewed notes use a translated allowlist. Unknown note codes remain hidden; remote text is never rendered as raw instructions or translation keys.

The current notes distinguish official_shared_context, official_input_limit, context_limit_unpublished, output_limit_unpublished, output_limit_unlimited, and long_decode_required. Old conservative-reservation notes are not emitted by the new 37-profile data.

Gemini4 displays its official 1,048,576 maximum output with a visible condition: normal synchronous output defaults to 65,536, and full long output needs background execution or continuation plus remaining shared context. Recording the official maximum does not add those modes to the existing Responses executor or claim current synchronous support for a million-token response.

Official reasoning metadata and executable reasoning options remain separate. Gemini2.5 official low/medium/high refers to the current Interactions documentation; the existing desktop Responses path remains automatic-only. Claude legacy fixed thinking budgets are not relabelled as equivalent official effort levels. The Grok4.5 xhigh/high equivalence note is limited to that profile.

none still means omitting the effort parameter and accepting the upstream default; it does not explicitly disable thinking. Automatic-only profiles retain the executable none level. Official reasoning metadata may be empty when no discrete effort enum is published, while executable model.reasoning_levels must be nonempty, ordered, and valid.

The card continues to explain Responses compatibility, structured-output limitations, and unavailable provider-native search. Those notes do not switch provider protocols or affect independent browser tools. The complete 37-item source/specification table and media boundaries are in DESKTOP_MODEL_PROFILES.md.

## Remote catalogue controls

The catalogue has its own settings component and action state, separate from the model draft. Its default source is:

https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/desktop-agent-profiles.json

The server accepts schema_version:1 with a revision and nonempty profiles. It refreshes on startup and every ten minutes, retaining accepted data in the existing settings record desktop_agent_profile_catalog_v1. The browser does not fetch or evaluate the remote feed directly.

The component displays source, revision, accepted profile count, last successful time, fallback state, and a safe failure message. Unknown server error details are not shown as remote instructions.

| API under /api/v1 | Frontend behavior |
| --- | --- |
| GET /admin/desktop/profiles | Load accepted reviewed profiles; strictly validate before enabling publish |
| GET /admin/desktop/profile-catalog | Read the current catalogue state |
| POST /admin/desktop/profile-catalog/refresh | Empty body, existing step-up flow, then reload profiles without submitting the model draft |
| PUT /admin/desktop/profile-catalog/source | Send only source_url through step-up; preserve the separate model draft |

Saving a source does not immediately replace accepted model data. An empty source pauses remote fetching while retaining the accepted catalogue. HTTPS/source validation, private-network and redirect rejection, the 2 MiB limit, source ownership, compatibility preflight, and CAS publication are enforced by the server.

Incoming entries cannot steal an existing ID/alias owner or change an existing profile's vendor. Remote omissions retain known entries and aliases. Validation, compatibility, fetch, and persistence failures keep the last accepted catalogue. Persistence does not change observable memory; only a successful commit publishes profiles, catalogue raw value and status under one lock. Every configuration read/write/bootstrap uses one server-side catalogue snapshot, preventing a response from combining different revisions. The final server catalogue regression passed 19/19 in both normal and race-detector runs.

Step-up cancellation, inactive tabs, unmount, and late responses keep existing generation fences. Catalogue refresh must not save the model draft, change its public identities, or trigger paid model calls.

## Protocol compatibility

The UI may publish models that need bootstrap protocol4; it must not invent smaller maximum values to make them fit an old client.

- Protocol1/2 retain their old positive-output, output-less-than-context shape. A configured incompatible model causes fail-closed service configuration.
- Protocol3 filters incompatible models into model_unavailable. A removed default stays null; no fallback model is silently selected.
- Protocol4 supports fixed maximum equal to context and explicit unlimited/unpublished zero sentinels while preserving protocol3 scope, empty catalogue, unavailable-model, and nullable-default behavior.

The new MiloCode Host requests protocol4 first and performs at most one protocol3 retry for the known old-service HTTP400 rejection shape. Authentication, permission, transport, throttling, service failures, and malformed successful responses do not trigger protocol downgrade. The same account/session and epoch checks guard both requests. The client maps only the positive operational default into existing optionSpecs; credentials stay in private Host material rather than public models, UI snapshots, or remote data.

## Current verification scope

The revised Go catalogue has been exported to output/desktop-agent-profiles-official.json with 37 entries and explicit nullable limits. Its targeted data/profile regression passed, including all official values, exact alias binding, fresh pointers/slices, preserved legal defaults, and bootstrap shapes.

The revised frontend passed 149 relevant tests covering profile validation, model drafts, catalogue controls, admin API wrappers and locale completeness. vue-tsc -b, changed-file ESLint and the Vite production build passed. A real-browser fixture used the actual Vue components and exported catalogue, mocking only the local catalogue management API. It verified the full DeepSeek context, Grok unknown/unlimited outputs, Gemini4 long-decode conditions, Gemini2.5 maximum-input labels, unsaved source preservation and Chinese/English 390px dark layouts without horizontal overflow or untranslated keys. Evidence is under output/playwright/agent-profiles-official-max-qa.json and the matching screenshot prefix. It did not change production settings or perform paid inference.

The stable remote JSON has been published on the independent model-profiles branch at commit 15465ab2cee2575bd04ef87b0d2080192437d21c. A real HTTPS GET returned 200 and matched all 37 reviewed entries byte-for-byte (SHA256 f03fd79e44a8959dd93f60a890a51a0dd93aeec22db103cfb12ea135b0ed85a0). The data branch contains only JSON and its maintenance README; it does not trigger a service release.

## Historical production release: 0.2.21-ds.17

The 2026-11-14 release 0.2.21-ds.17 was deployed and its original 37-profile page passed authenticated read-only production acceptance. A temporary unsaved DeepSeek draft auto-selected its profile, exposed low/high/max, and was removed by reloading saved settings; no model configuration or paid inference was submitted. Historical evidence is under output/deploy/fork-v0.2.21-17/.

That release used conservative context/input reservations and some default-based output caps. Those expressions are superseded by this contract's official raw values and explicit unknown/unlimited fields. Its deployment and browser results do not establish that this revision or its protocol4 client is live.

## Current release status

Only the remote data branch has been published. The server and client sources remain on their working branches; this service revision is not deployed. A new MiloCode package has not been produced, and no paid provider inference, long-decode, tool, or billing integration test has been performed for it.
