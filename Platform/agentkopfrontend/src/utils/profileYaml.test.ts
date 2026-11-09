import { describe, expect, it } from 'vitest';
import {
  buildProfileYaml,
  parseProfileYaml,
  patchProfileYamlTools,
  profileYamlToSessionProfile,
  tabsToSpaces,
} from './profileYaml';

describe('profileYaml conversion', () => {
  it('round-trips the supported profile fields', () => {
    const yaml = buildProfileYaml({
      systemPrompt: '你是视频创作助手',
      defaultModel: 'deepseek-v4-pro',
      tools: ['generate_image', 'compose_video'],
      pipelines: [{ name: 'storyboard_film', ref: 'pipeline:agentk.storyboard_film' }],
    });

    expect(parseProfileYaml(yaml)).toEqual({
      systemPrompt: '你是视频创作助手',
      defaultModel: 'deepseek-v4-pro',
      tools: ['generate_image', 'compose_video'],
      pipelines: [{ name: 'storyboard_film', ref: 'pipeline:agentk.storyboard_film' }],
    });
  });

  it('normalizes editor tabs before parsing YAML', () => {
    const yaml = 'system: |\n\t第一行\n\t第二行\ntools:\n\t- name: generate_image';

    expect(tabsToSpaces(yaml)).toContain('  第一行');
    expect(profileYamlToSessionProfile(yaml)).toMatchObject({
      system: '第一行\n第二行\n',
      tools: [{ name: 'generate_image' }],
    });
  });

  it('patches tools while preserving other profile fields', () => {
    const yaml = [
      'system: 保留这段提示词',
      'defaultModel: deepseek-v4-pro',
      'tools:',
      '  - name: old_tool',
    ].join('\n');

    const patched = patchProfileYamlTools(yaml, ['generate_image']);
    expect(parseProfileYaml(patched)).toEqual({
      systemPrompt: '保留这段提示词',
      defaultModel: 'deepseek-v4-pro',
      tools: ['generate_image'],
    });
  });

  it('keeps useful partial values while YAML is being edited', () => {
    const partial = [
      'system: |',
      '  临时提示词',
      'tools:',
      '  - generate_image',
      'invalid: [',
    ].join('\n');

    expect(parseProfileYaml(partial)).toMatchObject({
      systemPrompt: '临时提示词',
      tools: ['generate_image'],
    });
  });
});
