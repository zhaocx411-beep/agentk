package mediatype

import (
	"path"
	"strings"
)

const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"
)

var aliases = map[string]string{
	"audio/x-wav":    "audio/wav",
	"audio/wave":     "audio/wav",
	"audio/vnd.wave": "audio/wav",
	"audio/x-flac":   "audio/flac",
	"audio/x-aiff":   "audio/aiff",
	"audio/x-m4a":    "audio/m4a",
	"audio/mp4":      "audio/m4a",
}

var supported = map[string]string{
	"image/png": KindImage, "image/jpeg": KindImage, "image/webp": KindImage,
	"image/heic": KindImage, "image/heif": KindImage,
	"video/mp4": KindVideo, "video/mpeg": KindVideo, "video/quicktime": KindVideo,
	"video/avi": KindVideo, "video/x-flv": KindVideo, "video/mpg": KindVideo,
	"video/webm": KindVideo, "video/wmv": KindVideo, "video/3gpp": KindVideo,
	"audio/wav": KindAudio, "audio/mp3": KindAudio, "audio/aiff": KindAudio,
	"audio/aac": KindAudio, "audio/ogg": KindAudio, "audio/flac": KindAudio,
	"audio/mpeg": KindAudio, "audio/m4a": KindAudio, "audio/l16": KindAudio,
	"audio/opus": KindAudio, "audio/alaw": KindAudio, "audio/mulaw": KindAudio,
	"audio/webm": KindAudio,
}

var extensionMIME = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime",
	".avi": "video/avi", ".flv": "video/x-flv", ".wmv": "video/wmv",
	".mpeg": "video/mpeg", ".mpg": "video/mpg", ".3gp": "video/3gpp",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif",
	".wav": "audio/wav", ".mp3": "audio/mpeg", ".aif": "audio/aiff",
	".aiff": "audio/aiff", ".aac": "audio/aac", ".ogg": "audio/ogg",
	".oga": "audio/ogg", ".flac": "audio/flac", ".mpga": "audio/mpeg",
	".m4a": "audio/m4a", ".l16": "audio/l16", ".opus": "audio/opus",
	".alaw": "audio/alaw", ".mulaw": "audio/mulaw",
}

func Canonical(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	if canonical, ok := aliases[value]; ok {
		return canonical
	}
	return value
}

func Kind(value string) string { return supported[Canonical(value)] }

func Supported(value string) bool { return Kind(value) != "" }

func SupportedImage(value string) bool { return Kind(value) == KindImage }

func SupportedVideo(value string) bool { return Kind(value) == KindVideo }

func SupportedAudio(value string) bool { return Kind(value) == KindAudio }

// WebM 可包含音频或视频，需通过 Content-Type 消歧。
func FromExtension(name string) (string, bool) {
	ext := strings.ToLower(path.Ext(name))
	if ext == ".webm" || ext == ".mpeg" {
		return "", true
	}
	return extensionMIME[ext], false
}

func ManagedFileType(name string) string {
	mimeType, ambiguous := FromExtension(name)
	if ambiguous {
		return "static"
	}
	switch Kind(mimeType) {
	case KindVideo:
		return "video"
	case KindImage:
		return "image"
	default:
		return "static"
	}
}
