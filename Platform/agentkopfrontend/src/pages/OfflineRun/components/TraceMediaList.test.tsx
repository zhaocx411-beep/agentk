/** @vitest-environment jsdom */
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TraceMediaList } from './TraceMediaList';

afterEach(cleanup);

describe('TraceMediaList', () => {
  it('renders image, video and audio URLs found in a Trace payload', () => {
    const { container } = render(
      <TraceMediaList
        source={{
          artifacts: [
            { uri: 'https://example.com/result.mp4' },
            { url: 'https://example.com/result.wav' },
          ],
          message: '图片：https://example.com/result.png',
        }}
      />,
    );

    expect(container.querySelector('video')?.getAttribute('src')).toBe(
      'https://example.com/result.mp4',
    );
    expect(container.querySelector('audio')?.getAttribute('src')).toBe(
      'https://example.com/result.wav',
    );
    expect(container.querySelector('img')?.getAttribute('src')).toBe(
      'https://example.com/result.png',
    );
  });
});
