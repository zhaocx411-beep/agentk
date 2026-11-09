import { describe, expect, it, vi } from "vitest";

import { probeDurationBestEffort } from "./media";

describe("probeDurationBestEffort", () => {
  const file = {} as File;

  it("returns the browser-probed duration when available", async () => {
    const probe = vi.fn(async () => 12_345);
    await expect(probeDurationBestEffort(file, "video", probe)).resolves.toBe(12_345);
  });

  it("omits duration when browser probing fails so upload can continue", async () => {
    const probe = vi.fn(async () => {
      throw new Error("unsupported codec");
    });
    await expect(probeDurationBestEffort(file, "audio", probe)).resolves.toBeUndefined();
  });
});
