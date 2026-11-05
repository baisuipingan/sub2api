# Gemini image parameters

Verified on 2026-11-05 against Google’s official guide, model pages, and REST reference. The `ai.google.dev` pages currently redirect to `aistudio.google.com`; the rendered guide has separate Interactions and `generateContent` examples. This project uses `generateContent`.

## Model differences

| Model ID | Output sizes | Aspect ratios | Reference images |
| --- | --- | --- | --- |
| `gemini-2.5-flash-image` | Fixed output dimensions for each ratio, approximately 1K; no configurable `imageSize` | Standard 10 | Google recommends up to 3 for best results; this is not described as a hard API maximum |
| `gemini-3-pro-image` | `1K`, `2K`, `4K`; default `1K` | Standard 10 | Up to 14 total; guide separately describes 6 high-fidelity objects, 5 character images, and 3 style images |
| `gemini-3.1-flash-image` | `512`, `1K`, `2K`, `4K`; default `1K` | Extended 14 | Up to 14; up to 10 objects and 4 characters |
| `gemini-3.1-flash-lite-image` | Only 1K; the model page explicitly excludes 2K/4K | Model page says 14 but enumerates only the standard 10; use those 10 until clarified | Not optimized for multiple reference inputs; no numerical maximum verified on this model page |
| `gemini-nano-banana-2.1` | `1K`, `2K`, `4K`; default `1K`; no `512` | Extended 14 | Up to 14; up to 10 objects and 4 characters |

Standard 10: `1:1`, `2:3`, `3:2`, `3:4`, `4:3`, `4:5`, `5:4`, `9:16`, `16:9`, `21:9`.

Extended 14 adds `1:4`, `4:1`, `1:8`, `8:1`.

These profiles describe controls for models actually returned by the gateway. They must not create new model options or rename public IDs. `gemini-2.5-flash-image-preview` is explicitly deprecated on its model page. The current 3 Pro and 3.1 Flash model pages list their stable IDs; no new preview variants are assumed.

## Native REST request

```json
{
  "contents": [{
    "role": "user",
    "parts": [
      { "text": "Draw a landscape" },
      { "inlineData": { "mimeType": "image/png", "data": "BASE64" } }
    ]
  }],
  "generationConfig": {
    "responseModalities": ["IMAGE"],
    "imageConfig": {
      "aspectRatio": "16:9",
      "imageSize": "2K"
    }
  }
}
```

- Endpoint: `/v1beta/models/{publicModel}:generateContent`.
- `imageConfig.aspectRatio` is optional; absent means the model chooses based on input images. Keep an explicit selected ratio directly, especially for `1:8`/`8:1`, rather than inferring it from dimensions constrained for another provider.
- `imageConfig.imageSize` accepts `512`, `1K`, `2K`, `4K` at the schema level. Model support is narrower. UI `0.5K` maps to REST string `"512"`; do not send `"0.5K"` or lowercase `k`.
- Fixed-size 2.5 Flash and 3.1 Flash Lite profiles omit `imageSize`; `auto` also omits it. Missing configuration defaults to 1K on models with configurable output resolution.
- The native `ImageConfig` schema documents no GPT-style `quality`, `output_format`, `output_compression`, `moderation`, `background`, or mask parameter. Do not copy those controls into native requests.
- The guide warns that the model may not follow a requested number of output images. There is no native Images API `n` parameter here; the existing workbench generates multiple images through separate calls.
- A response `Part` may have `thought: true`. Intermediate thought images are not final output images; exclude them from the workbench result count.
- Conversational follow-up requires preserving model response thought signatures. This workbench sends independent reference-image requests and does not claim to preserve a native Gemini conversation.

## Gateway transport

The Gemini native gateway forwards `generationConfig` in the JSON body. The OpenAI-compatible Images path for a Gemini-named upstream instead forwards an Images request to that configured provider; it does not convert `size` into native `imageConfig`. Therefore native Gemini capabilities such as `512` and exact ratio fields cannot be assumed for every third-party Images transport.

## Sources

- [Image generation guide](https://ai.google.dev/gemini-api/docs/image-generation) → [current guide](https://aistudio.google.com/docs/image-generation), especially *Limitations*, *Aspect ratios and image size*, and *Thinking process*.
- [generateContent guide view](https://aistudio.google.com/docs/image-generation?generate-content=true).
- [REST generateContent reference](https://ai.google.dev/api/generate-content#ImageConfig) → [current reference](https://aistudio.google.com/api-reference/generate-content), `GenerationConfig.imageConfig` and `Part.thought`.
- [2.5 Flash Image](https://aistudio.google.com/docs/models/gemini-2.5-flash-image).
- [3 Pro Image](https://aistudio.google.com/docs/models/gemini-3-pro-image).
- [3.1 Flash Image](https://aistudio.google.com/docs/models/gemini-3.1-flash-image).
- [3.1 Flash Lite Image](https://aistudio.google.com/docs/models/gemini-3.1-flash-lite-image).
- [Nano Banana 2.1](https://aistudio.google.com/docs/models/gemini-nano-banana-2.1).

Reviewable source text, retrieval timestamps, and hashes are saved under `output/docs/image-providers/gemini-*.txt`; the exact resolution tables are saved there as `*-sizes-excerpt.txt`.
