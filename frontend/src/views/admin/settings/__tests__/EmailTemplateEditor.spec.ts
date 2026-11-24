import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import EmailTemplateEditor from "../EmailTemplateEditor.vue";

const event = "balance.recharge_success";
const subject = "余额充值成功";
const savedHTML = "<p>充值完成。</p><p>订单号：{{order_id}}</p>";
const draftHTML = "<p>充值完成。</p>";
const { getEmailTemplate, previewEmailTemplate, updateEmailTemplate } = vi.hoisted(() => ({
  getEmailTemplate: vi.fn(),
  previewEmailTemplate: vi.fn(),
  updateEmailTemplate: vi.fn(),
}));

vi.mock("@/api", () => ({
  adminAPI: {
    settings: {
      getEmailTemplates: () => Promise.resolve({ events: [event], locales: ["zh", "en"] }),
      getEmailTemplate,
      previewEmailTemplate,
      updateEmailTemplate,
    },
  },
}));

vi.mock("@/stores", () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}));

vi.mock("vue-i18n", () => ({
  useI18n: () => ({ t: (key: string) => key, locale: { value: "zh-CN" } }),
}));

describe("EmailTemplateEditor", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.resetAllMocks();
  });

  it("previews the latest draft and ignores an older request resolved after it", async () => {
    vi.useFakeTimers();
    getEmailTemplate.mockResolvedValue({ event, locale: "zh", subject, html: savedHTML });
    let resolveSavedPreview!: (value: { subject: string; html: string }) => void;
    previewEmailTemplate
      .mockImplementationOnce(() => new Promise(resolve => { resolveSavedPreview = resolve; }))
      .mockResolvedValueOnce({ subject, html: draftHTML });
    const wrapper = mount(EmailTemplateEditor);
    try {
      await flushPromises();
      await vi.runOnlyPendingTimersAsync();
      expect(previewEmailTemplate).toHaveBeenCalledTimes(1);
      expect(previewEmailTemplate).toHaveBeenCalledWith({ event, locale: "zh", subject, html: savedHTML });

      await wrapper.get("#email-template-html").setValue(draftHTML);
      await vi.advanceTimersByTimeAsync(299);
      expect(previewEmailTemplate).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(previewEmailTemplate).toHaveBeenLastCalledWith({ event, locale: "zh", subject, html: draftHTML });
      expect(wrapper.get("iframe").attributes("srcdoc")).toBe(draftHTML);

      resolveSavedPreview({ subject, html: savedHTML.replace("{{order_id}}", "1024") });
      await flushPromises();
      expect(wrapper.get("iframe").attributes("srcdoc")).toBe(draftHTML);
    } finally {
      wrapper.unmount();
    }
  });

  it("disables template editing if loading the selected language fails", async () => {
    getEmailTemplate
      .mockResolvedValueOnce({ event, locale: "zh", subject, html: savedHTML })
      .mockRejectedValueOnce(new Error("load failure"));
    const wrapper = mount(EmailTemplateEditor);
    try {
      await flushPromises();
      await wrapper.get("#email-template-html").setValue(draftHTML);
      await wrapper.get("#email-template-locale").setValue("en");
      await flushPromises();

      const field = wrapper.get<HTMLTextAreaElement>("#email-template-html");
      expect(field.element.value).toBe("");
      expect(field.element.disabled).toBe(true);
      await wrapper.vm.saveChanges();
      expect(updateEmailTemplate).not.toHaveBeenCalled();
    } finally {
      wrapper.unmount();
    }
  });
});
