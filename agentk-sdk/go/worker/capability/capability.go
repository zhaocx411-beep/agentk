// Package capability declares and invokes AgentK capabilities implemented by
// Temporal workflows and activities.
package capability

import "encoding/json"

// Capability describes a Temporal workflow or activity that an AgentK Worker
// publishes. The publish method supplies the implementation kind, and the
// Worker supplies the task queue.
type Capability struct {
	// ID is the globally unique capability ID. Contributor Workers must use
	// contrib.<owner>.<name>; IDs may contain lowercase letters, digits, and
	// underscores and must not exceed 128 characters.
	ID string

	// TemporalType is the registered Temporal workflow or activity type.
	TemporalType string

	// Description explains the capability to users and, when Tool.Description
	// is empty, to the model that selects the Tool.
	Description string

	// InputSchema is the JSON Schema for the capability input. Its top-level
	// type must be object.
	InputSchema json.RawMessage

	// OutputSchema is the JSON Schema for the capability result.
	OutputSchema json.RawMessage

	// TimeoutSeconds is the maximum execution duration applied when AgentK
	// invokes the capability. It must be between 1 second and 24 hours.
	TimeoutSeconds int64

	// Tool publishes the capability as a model-callable Tool when non-nil.
	Tool *Tool
}

// Tool configures how a published capability is presented as a model-callable
// Tool.
type Tool struct {
	// Description overrides the Capability description for Tool selection. An
	// empty value inherits Capability.Description.
	Description string

	// MaxAttempts is the maximum number of Tool attempts. Zero leaves the value
	// unspecified; negative values are invalid.
	MaxAttempts int32
}

// Artifact describes an output produced by a capability.
type Artifact struct {
	// Type identifies the kind of artifact, such as image or video.
	Type string `json:"type"`

	// URI locates the artifact for downstream consumers.
	URI string `json:"uri"`

	// Description is an optional human-readable summary of the artifact.
	Description string `json:"description,omitempty"`

	// Model identifies the model that produced the artifact, when applicable.
	Model string `json:"model,omitempty"`

	// Params records optional model or generation parameters.
	Params map[string]any `json:"params,omitempty"`
}
