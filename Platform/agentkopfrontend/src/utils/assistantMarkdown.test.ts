import { describe, expect, it } from 'vitest';
import { restoreLegacyHeadingIcons, safeMarkdownUrl, truncateDebugText } from './assistantMarkdown';

describe('restoreLegacyHeadingIcons', () => {
  it('restores known emoji placeholders in markdown headings', () => {
    const markdown = ['## ? 介绍视频', '### ? 视频参数', '### ? 内容概要'].join('\n');

    expect(restoreLegacyHeadingIcons(markdown)).toBe(
      ['## 🎬 介绍视频', '### ⚙️ 视频参数', '### 📝 内容概要'].join('\n'),
    );
  });

  it('uses a stable fallback for unknown headings without changing real questions', () => {
    const markdown = ['## ? 未知标题', '这个结果可以使用吗？', '是否继续?'].join('\n');
    expect(restoreLegacyHeadingIcons(markdown)).toBe(
      ['## ◆ 未知标题', '这个结果可以使用吗？', '是否继续?'].join('\n'),
    );
  });

  it('restores common delivery headings from historical replies', () => {
    const markdown = [
      '## ? 成片交付',
      '### ? 脚本回顾',
      '### ? 成片',
      '### ? 分镜内容',
      '### ? 成片评价',
      '### ? 质检细则',
    ].join('\n');

    expect(restoreLegacyHeadingIcons(markdown)).toBe(
      [
        '## 🎬 成片交付',
        '### 📋 脚本回顾',
        '### 🎞️ 成片',
        '### 🎞️ 分镜内容',
        '### ✅ 成片评价',
        '### ✅ 质检细则',
      ].join('\n'),
    );
  });

  it('restores sequence and decision markers only in known markdown contexts', () => {
    const markdown = [
      '| 序号 | 步骤 |',
      '| ?1 | 步骤7 |',
      '| ?20 | 步骤24 |',
      '步骤7（用户确认）← ? 决策点1',
      '普通问题?1是否继续',
    ].join('\n');

    expect(restoreLegacyHeadingIcons(markdown)).toBe(
      [
        '| 序号 | 步骤 |',
        '| ① | 步骤7 |',
        '| ⑳ | 步骤24 |',
        '步骤7（用户确认）← ◆ 决策点1',
        '普通问题?1是否继续',
      ].join('\n'),
    );
  });

  it('restores damaged icons on semantic list labels', () => {
    const markdown = [
      '- ? **风格**：中国传统水墨画',
      '- ? **机甲：**融合青铜器纹样',
      '- 🏔️ **场景**：云雾山巅',
      '- ? 画幅比例：1:1 正方形',
      '- ? 是否继续?',
    ].join('\n');

    expect(restoreLegacyHeadingIcons(markdown)).toBe(
      [
        '- 🎨 **风格**：中国传统水墨画',
        '- 🤖 **机甲：**融合青铜器纹样',
        '- 🏔️ **场景**：云雾山巅',
        '- 🖼️ 画幅比例：1:1 正方形',
        '- ? 是否继续?',
      ].join('\n'),
    );
  });
});

describe('assistant markdown safety', () => {
  it('allows web links and rejects executable protocols', () => {
    expect(safeMarkdownUrl('https://example.com/video.mp4')).toBe('https://example.com/video.mp4');
    expect(safeMarkdownUrl('/session-review/ses_1')).toBe('/session-review/ses_1');
    expect(safeMarkdownUrl('javascript:alert(1)')).toBe('');
    expect(safeMarkdownUrl('data:text/html,unsafe')).toBe('');
  });

  it('caps oversized debug payloads', () => {
    const result = truncateDebugText('x'.repeat(100), 20);
    expect(result.startsWith('x'.repeat(20))).toBe(true);
    expect(result).toContain('已截断 80 个字符');
  });
});
