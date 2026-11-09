package videoprobe

import (
	"encoding/json"
	"math"
	"testing"
)

func TestNormalizeFFprobeOutput(t *testing.T) {
	const fixture = `{
		"streams": [
			{
				"index": 0,
				"codec_name": "h264",
				"codec_long_name": "H.264",
				"profile": "High",
				"codec_type": "video",
				"codec_tag_string": "avc1",
				"width": 1920,
				"height": 1080,
				"pix_fmt": "yuv420p",
				"level": 40,
				"avg_frame_rate": "30000/1001",
				"r_frame_rate": "30/1",
				"duration": "12.345",
				"bit_rate": "4000000",
				"time_base": "1/30000",
				"tags": {"language": "und", "title": "Main"},
				"side_data_list": [{"rotation": 90}]
			},
			{
				"index": 1,
				"codec_name": "aac",
				"codec_type": "audio",
				"sample_fmt": "fltp",
				"sample_rate": "48000",
				"channels": 2,
				"channel_layout": "stereo",
				"duration": "12.300",
				"bit_rate": "192000",
				"tags": {"language": "eng"}
			},
			{
				"index": 2,
				"codec_name": "mov_text",
				"codec_type": "subtitle",
				"tags": {"language": "zho"}
			}
		],
		"format": {
			"format_name": "mov,mp4,m4a,3gp,3g2,mj2",
			"format_long_name": "QuickTime / MOV",
			"duration": "12.345",
			"bit_rate": "4200000",
			"probe_score": 100,
			"tags": {"major_brand": "isom"}
		}
	}`
	var decoded ffprobeOutput
	if err := json.Unmarshal([]byte(fixture), &decoded); err != nil {
		t.Fatal(err)
	}
	out := normalize(decoded, 6_480_000, json.RawMessage(fixture))

	if out.FileSizeBytes != 6_480_000 || out.DurationSecs != 12.345 ||
		out.Format.BitRate != 4_200_000 {
		t.Fatalf("summary = %+v", out)
	}
	if len(out.VideoStreams) != 1 || len(out.AudioStreams) != 1 ||
		len(out.SubtitleStreams) != 1 || len(out.OtherStreams) != 0 {
		t.Fatalf("stream groups = video:%d audio:%d subtitle:%d other:%d",
			len(out.VideoStreams), len(out.AudioStreams), len(out.SubtitleStreams), len(out.OtherStreams))
	}
	video := out.VideoStreams[0]
	if video.Width != 1920 || video.Height != 1080 ||
		video.DisplayWidth != 1080 || video.DisplayHeight != 1920 || video.Rotation != 90 {
		t.Fatalf("video dimensions = %+v", video)
	}
	if math.Abs(video.FrameRate-29.97002997) > 0.000001 || video.RealFrameRate != 30 {
		t.Fatalf("frame rates = %f / %f", video.FrameRate, video.RealFrameRate)
	}
	audio := out.AudioStreams[0]
	if audio.SampleRate != 48000 || audio.Channels != 2 || audio.Language != "eng" {
		t.Fatalf("audio = %+v", audio)
	}
}

func TestSanitizedRawReplacesTemporaryFilename(t *testing.T) {
	raw, err := sanitizedRaw([]byte(`{"format":{"filename":"/tmp/private.mp4"},"streams":[]}`),
		"https://example.com/video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"format":{"filename":"https://example.com/video.mp4"},"streams":[]}` {
		t.Fatalf("raw = %s", raw)
	}
}
