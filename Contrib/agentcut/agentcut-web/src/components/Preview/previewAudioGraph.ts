type Binding = {
  source: MediaElementAudioSourceNode;
  gain: GainNode;
};

export class PreviewAudioGraph {
  private context: AudioContext | null = null;
  private bindings = new Map<HTMLMediaElement, Binding>();
  private elements = new Set<HTMLMediaElement>();
  private activationAttempted = false;
  private activated = false;
  private requiresWebAudio = false;
  readonly projectId: string | undefined;

  constructor(projectId?: string) {
    this.projectId = projectId;
  }

  /**
   * 必须直接在 pointer/keyboard 用户手势中调用。浏览器会暂停在普通 React
   * effect 中创建或 resume 的 AudioContext；媒体一旦接入该暂停图就会完全无声。
   */
  async activate(): Promise<boolean> {
    this.activationAttempted = true;
    const AudioContextConstructor =
      window.AudioContext ??
      (window as typeof window & { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;
    if (!AudioContextConstructor) {
      this.activated = false;
      return false;
    }
    try {
      this.context ??= new AudioContextConstructor();
      if (this.context.state === "suspended") await this.context.resume();
      this.activated = this.context.state === "running";
      return this.activated;
    } catch {
      this.activated = false;
      return false;
    }
  }

  applyGain(element: HTMLMediaElement, gain: number): boolean {
    const safeGain = Math.max(0, gain);
    this.elements.add(element);
    // 常规音量和多轨混音直接走媒体元素原生输出，避免把默认播放链路接入
    // MediaElementAudioSourceNode 后受 AudioContext/CORS 状态影响而整体静音。
    // 只有确实需要超过 1 的增益时，才将全部活动源统一切到 Web Audio。
    if (safeGain > 1) this.requiresWebAudio = true;
    let binding = this.bindings.get(element);
    if (!binding && this.requiresWebAudio && this.activated && this.context) {
      try {
        const source = this.context.createMediaElementSource(element);
        const gainNode = this.context.createGain();
        source.connect(gainNode);
        gainNode.connect(this.context.destination);
        binding = { source, gain: gainNode };
        this.bindings.set(element, binding);
      } catch {
        element.muted = false;
        element.volume = Math.min(1, safeGain);
        return safeGain <= 1;
      }
    }
    if (binding) {
      element.muted = false;
      element.volume = 1;
      binding.gain.gain.value = safeGain;
      return true;
    }
    element.muted = safeGain === 0;
    element.volume = Math.min(1, safeGain);
    return safeGain <= 1 || !this.activationAttempted;
  }

  playAll(
    onError: (error: unknown) => void,
    onStarted: () => void = () => {},
  ): void {
    for (const element of this.elements) {
      if (!element.isConnected) {
        this.release(element);
        continue;
      }
      void element.play().then(onStarted).catch(onError);
    }
  }

  release(element: HTMLMediaElement): void {
    this.elements.delete(element);
    const binding = this.bindings.get(element);
    if (!binding) return;
    binding.source.disconnect();
    binding.gain.disconnect();
    this.bindings.delete(element);
  }

  dispose(): void {
    for (const [element] of this.bindings) this.release(element);
    if (this.context) void this.context.close().catch(() => {});
    this.context = null;
    this.elements.clear();
    this.activationAttempted = false;
    this.activated = false;
    this.requiresWebAudio = false;
  }
}

