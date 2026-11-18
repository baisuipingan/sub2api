import { describe, expect, it } from "vitest";

import {
  normalizeGroupOpenAIFast,
  supportsGroupOpenAIFast,
} from "../groupsOpenAIFast";
import en from "@/i18n/locales/en/admin/overview";
import zh from "@/i18n/locales/zh/admin/overview";

describe("groupsOpenAIFast", () => {
  it("supports OpenAI and composite groups", () => {
    expect(supportsGroupOpenAIFast("openai")).toBe(true);
    expect(supportsGroupOpenAIFast("composite")).toBe(true);
    expect(supportsGroupOpenAIFast("anthropic")).toBe(false);
  });

  it("clears stale enabled state on unsupported platforms", () => {
    expect(normalizeGroupOpenAIFast("openai", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("composite", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("anthropic", true)).toBe(false);
    expect(normalizeGroupOpenAIFast("openai", false)).toBe(false);
  });

  it("provides localized Ultra Fast switch labels naming the supported models", () => {
    for (const locale of [zh, en]) {
      expect(locale.groups.openaiFast).toMatchObject({
        forceUltrafast: expect.stringContaining("ultrafast"),
        ultrafastHint: expect.any(String),
      });
      for (const modelOrPlan of [
        "GPT-6.2",
        "Sol",
        "Astra",
        "Pro 200",
        "Pro 500",
        "GPT-6 Astra",
        "GPT-6.1 Sol",
      ]) {
        expect(locale.groups.openaiFast.ultrafastHint).toContain(modelOrPlan);
      }
    }
  });

  it("explains the GPT-6.2 promotional price and its Pacific time deadline", () => {
    expect(zh.groups.openaiFast.ultrafastHint).toContain(
      "Pro 200 支持 Sol，Pro 500 支持 Sol 和 Astra",
    );
    expect(en.groups.openaiFast.ultrafastHint).toContain(
      "Pro 200 includes GPT-6.2 Sol Ultrafast. Pro 500 includes both Sol and Astra Ultrafast",
    );
    expect(zh.groups.openaiFast.ultrafastHint).toContain(
      "太平洋时间 2027 年 2 月 28 日及之前按标准价 3 倍计费，之后为 6 倍",
    );
    expect(en.groups.openaiFast.ultrafastHint).toContain(
      "3× Standard through February 28, 2027 (Pacific time), then 6×",
    );
  });

  it("provides localized switch labels", () => {
    expect(zh.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
    expect(en.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
  });
});
