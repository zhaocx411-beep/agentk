package tools

import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// Definition 把一个工具的 capability 契约和 Activity 实现绑在一起，
// worker 入口据此统一注册。
type Definition struct {
	Capability capability.Capability
	// Handler 返回要注册的 Activity 函数（TemporalType 作为注册名）。
	Handler func(a *Activities) any
}

// Definitions 返回全部 contrib.agentcut.* 工具。
func Definitions() []Definition {
	return []Definition{
		{
			Capability: capability.Capability{
				ID:             CapTimelineGet,
				TemporalType:   CapTimelineGet,
				Description:    descGet,
				InputSchema:    capability.SchemaOf[GetInput](),
				OutputSchema:   capability.SchemaOf[GetOutput](),
				TimeoutSeconds: 60,
				Tool:           &capability.Tool{},
			},
			Handler: func(a *Activities) any { return a.TimelineGet },
		},
		{
			Capability: capability.Capability{
				ID:             CapTimelineEditClips,
				TemporalType:   CapTimelineEditClips,
				Description:    descEditClips,
				InputSchema:    capability.SchemaOf[EditInput](),
				OutputSchema:   capability.SchemaOf[EditOutput](),
				TimeoutSeconds: 120,
				Tool:           &capability.Tool{MaxAttempts: 1},
			},
			Handler: func(a *Activities) any { return a.TimelineEditClips },
		},
		{
			Capability: capability.Capability{
				ID:             CapTimelineEditTracks,
				TemporalType:   CapTimelineEditTracks,
				Description:    descEditTracks,
				InputSchema:    capability.SchemaOf[EditInput](),
				OutputSchema:   capability.SchemaOf[EditOutput](),
				TimeoutSeconds: 120,
				Tool:           &capability.Tool{MaxAttempts: 1},
			},
			Handler: func(a *Activities) any { return a.TimelineEditTracks },
		},
		{
			Capability: capability.Capability{
				ID:             CapTimelineEditMedia,
				TemporalType:   CapTimelineEditMedia,
				Description:    descEditMedia,
				InputSchema:    capability.SchemaOf[EditInput](),
				OutputSchema:   capability.SchemaOf[EditOutput](),
				TimeoutSeconds: 120,
				Tool:           &capability.Tool{MaxAttempts: 1},
			},
			Handler: func(a *Activities) any { return a.TimelineEditMedia },
		},
		{
			Capability: capability.Capability{
				ID:             CapTimelineSetCaptions,
				TemporalType:   CapTimelineSetCaptions,
				Description:    descSetCaptions,
				InputSchema:    capability.SchemaOf[SetCaptionsInput](),
				OutputSchema:   capability.SchemaOf[EditOutput](),
				TimeoutSeconds: 120,
				Tool:           &capability.Tool{MaxAttempts: 1},
			},
			Handler: func(a *Activities) any { return a.TimelineSetCaptions },
		},
		{
			Capability: capability.Capability{
				ID:             CapTimelineEditTranscript,
				TemporalType:   CapTimelineEditTranscript,
				Description:    descEditTranscript,
				InputSchema:    capability.SchemaOf[TranscriptEditInput](),
				OutputSchema:   capability.SchemaOf[TranscriptEditOutput](),
				TimeoutSeconds: 120,
				Tool:           &capability.Tool{MaxAttempts: 1},
			},
			Handler: func(a *Activities) any { return a.TimelineEditTranscript },
		},
	}
}
