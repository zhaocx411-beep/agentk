import { describe, expect, it } from 'vitest';
import type { OfficialToolParameter } from '@/types/officialTools';
import {
  argsToParamFormValues,
  collectToolDefaults,
} from './toolDefaultsUtils';

const operationsParam: OfficialToolParameter = {
  name: 'operations',
  type: 'array<string>',
  required: true,
};

describe('collectToolDefaults', () => {
  it('keeps a JSON object as one object-array item', () => {
    const values = {
      operations: `{
        "type": "trim",
        "startSeconds": 0,
        "durationSeconds": 3
      }`,
    };

    expect(collectToolDefaults([operationsParam], values)).toEqual({
      operations: [
        {
          type: 'trim',
          startSeconds: 0,
          durationSeconds: 3,
        },
      ],
    });
  });

  it('keeps a JSON array instead of splitting formatted lines', () => {
    const values = {
      operations: JSON.stringify([
        { type: 'trim', startSeconds: 0, durationSeconds: 3 },
        { type: 'mute', startSeconds: 3, durationSeconds: 2 },
      ], null, 2),
    };

    expect(collectToolDefaults([operationsParam], values).operations).toEqual([
      { type: 'trim', startSeconds: 0, durationSeconds: 3 },
      { type: 'mute', startSeconds: 3, durationSeconds: 2 },
    ]);
  });

  it('still supports newline and comma separated string arrays', () => {
    expect(
      collectToolDefaults(
        [{ name: 'urls', type: 'array<string>' }],
        { urls: 'https://a.example/a.mp4\nhttps://b.example/b.mp4, https://c.example/c.mp4' },
      ),
    ).toEqual({
      urls: [
        'https://a.example/a.mp4',
        'https://b.example/b.mp4',
        'https://c.example/c.mp4',
      ],
    });
  });

  it('does not submit malformed JSON as line fragments', () => {
    expect(
      collectToolDefaults([operationsParam], {
        operations: '[{"type":"trim"',
      }),
    ).toEqual({});
  });
});

describe('argsToParamFormValues', () => {
  it('formats object-array history as editable JSON', () => {
    const values = argsToParamFormValues([operationsParam], {
      operations: [{ type: 'trim', startSeconds: 0, durationSeconds: 3 }],
    });

    expect(JSON.parse(String(values.operations))).toEqual([
      { type: 'trim', startSeconds: 0, durationSeconds: 3 },
    ]);
  });
});
