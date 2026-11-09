// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";

import { PreviewAudioGraph } from "./previewAudioGraph";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("PreviewAudioGraph", () => {
  it("uses media volume up to one and falls back clearly without Web Audio", async () => {
    const original = window.AudioContext;
    Object.defineProperty(window, "AudioContext", { configurable: true, value: undefined });
    const graph = new PreviewAudioGraph();
    const element = document.createElement("audio");
    expect(graph.applyGain(element, 0.5)).toBe(true);
    expect(element.volume).toBe(0.5);
    expect(await graph.activate()).toBe(false);
    expect(graph.applyGain(element, 1.5)).toBe(false);
    expect(element.volume).toBe(1);
    Object.defineProperty(window, "AudioContext", { configurable: true, value: original });
  });

  it("binds every overlapping source to one graph after activation", async () => {
    const disconnectSource = vi.fn();
    const disconnectGain = vi.fn();
    const close = vi.fn().mockResolvedValue(undefined);
    const createMediaElementSource = vi.fn(() => ({
      connect: vi.fn(),
      disconnect: disconnectSource,
    }));
    const createGain = vi.fn(() => ({
      connect: vi.fn(),
      disconnect: disconnectGain,
      gain: { value: 1 },
    }));
    class MockAudioContext {
      destination = {};
      state = "running";
      createMediaElementSource = createMediaElementSource;
      createGain = createGain;
      resume = vi.fn().mockResolvedValue(undefined);
      close = close;
    }
    Object.defineProperty(window, "AudioContext", {
      configurable: true,
      value: MockAudioContext,
    });
    const graph = new PreviewAudioGraph();
    const element = document.createElement("video");
    const secondElement = document.createElement("audio");
    expect(graph.applyGain(element, 1.5)).toBe(true);
    expect(createMediaElementSource).not.toHaveBeenCalled();
    expect(await graph.activate()).toBe(true);
    expect(graph.applyGain(element, 1.5)).toBe(true);
    expect(graph.applyGain(element, 1.2)).toBe(true);
    expect(graph.applyGain(secondElement, 0.5)).toBe(true);
    expect(createMediaElementSource).toHaveBeenCalledTimes(2);
    graph.release(element);
    graph.release(secondElement);
    expect(disconnectSource).toHaveBeenCalled();
    expect(disconnectGain).toHaveBeenCalled();
    graph.dispose();
    expect(close).toHaveBeenCalled();
  });

  it("keeps normal gain on native output and starts registered media directly", async () => {
    const createMediaElementSource = vi.fn();
    class MockAudioContext {
      destination = {};
      state = "running";
      createMediaElementSource = createMediaElementSource;
      createGain = vi.fn();
      resume = vi.fn().mockResolvedValue(undefined);
      close = vi.fn().mockResolvedValue(undefined);
    }
    Object.defineProperty(window, "AudioContext", {
      configurable: true,
      value: MockAudioContext,
    });
    const graph = new PreviewAudioGraph();
    const element = document.createElement("video");
    document.body.append(element);
    const play = vi.spyOn(element, "play").mockResolvedValue(undefined);

    expect(await graph.activate()).toBe(true);
    expect(graph.applyGain(element, 0.5)).toBe(true);
    expect(element.volume).toBe(0.5);
    expect(element.muted).toBe(false);
    expect(createMediaElementSource).not.toHaveBeenCalled();

    graph.playAll(vi.fn());
    expect(play).toHaveBeenCalledTimes(1);
    graph.dispose();
    element.remove();
  });

  it("resumes a suspended context only through explicit activation", async () => {
    const resume = vi.fn().mockImplementation(function (this: { state: string }) {
      this.state = "running";
      return Promise.resolve();
    });
    class SuspendedAudioContext {
      destination = {};
      state = "suspended";
      createMediaElementSource = vi.fn();
      createGain = vi.fn();
      resume = resume;
      close = vi.fn().mockResolvedValue(undefined);
    }
    Object.defineProperty(window, "AudioContext", {
      configurable: true,
      value: SuspendedAudioContext,
    });

    const graph = new PreviewAudioGraph();
    expect(await graph.activate()).toBe(true);
    expect(resume).toHaveBeenCalledTimes(1);
    graph.dispose();
  });
});

