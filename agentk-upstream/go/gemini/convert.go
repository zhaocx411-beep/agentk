package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/mediatype"
)

type requestBody struct {
	SystemInstruction *content     `json:"systemInstruction,omitempty"`
	Contents          []content    `json:"contents"`
	Tools             []geminiTool `json:"tools,omitempty"`
}
type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}
type part struct {
	Text             *string           `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FileData         *fileData         `json:"fileData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}
type inlineData struct {
	MIMEType    string `json:"mimeType"`
	DisplayName string `json:"displayName,omitempty"`
	Data        string `json:"data"`
}
type fileData struct {
	MIMEType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}
type functionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}
type functionResponse struct {
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name"`
	Response map[string]any         `json:"response"`
	Parts    []functionResponsePart `json:"parts,omitempty"`
}
type functionResponsePart struct {
	InlineData *inlineData `json:"inlineData,omitempty"`
}
type geminiTool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}
type functionDeclaration struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	ParametersJSONSchema map[string]any `json:"parametersJsonSchema,omitempty"`
}
type callMeta struct{ name, providerID string }

func buildRequest(req chat.Request) (requestBody, error) {
	out := requestBody{}
	if len(req.OutputSchema) > 0 {
		return out, fmt.Errorf("output schema is not implemented for Gemini")
	}
	if req.System != "" {
		text := req.System
		out.SystemInstruction = &content{Parts: []part{{Text: &text}}}
	}
	calls := make(map[string]callMeta)
	for i, message := range req.Messages {
		converted, found, err := buildContent(message, calls)
		if err != nil {
			return requestBody{}, fmt.Errorf("message %d: %w", i, err)
		}
		if found {
			last := len(out.Contents) - 1
			if last >= 0 && functionResponsesOnly(out.Contents[last]) && functionResponsesOnly(converted) {
				out.Contents[last].Parts = append(out.Contents[last].Parts, converted.Parts...)
			} else {
				out.Contents = append(out.Contents, converted)
			}
		}
	}
	if len(req.Tools) > 0 {
		decls := make([]functionDeclaration, 0, len(req.Tools))
		for _, def := range req.Tools {
			decl := functionDeclaration{Name: def.Name, Description: def.Description}
			if len(def.InputSchema) > 0 {
				if err := json.Unmarshal(def.InputSchema, &decl.ParametersJSONSchema); err != nil {
					return requestBody{}, fmt.Errorf("tool %s: decode input schema: %w", def.Name, err)
				}
			}
			decls = append(decls, decl)
		}
		out.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}
	return out, nil
}

func functionResponsesOnly(v content) bool {
	if v.Role != "user" || len(v.Parts) == 0 {
		return false
	}
	for _, p := range v.Parts {
		if p.FunctionResponse == nil {
			return false
		}
	}
	return true
}

func buildContent(message chat.Message, calls map[string]callMeta) (content, bool, error) {
	switch message.Role {
	case chat.RoleUser:
		parts, err := mediaParts(message)
		return content{Role: "user", Parts: parts}, len(parts) > 0, err
	case chat.RoleAssistant:
		parts, err := assistantParts(message, calls)
		return content{Role: "model", Parts: parts}, len(parts) > 0, err
	case chat.RoleTool:
		meta, ok := calls[message.ToolCallID]
		if !ok {
			return content{}, false, fmt.Errorf("tool result %q has no preceding function call", message.ToolCallID)
		}
		var decoded any
		if err := json.Unmarshal([]byte(message.Text), &decoded); err != nil {
			decoded = message.Text
		}
		response, ok := decoded.(map[string]any)
		if !ok || response == nil {
			response = map[string]any{"result": decoded}
		}
		wire := &functionResponse{ID: meta.providerID, Name: meta.name, Response: response}
		if len(message.Media) > 0 {
			refs := make([]map[string]string, 0, len(message.Media))
			for i, image := range message.Media {
				if len(image.Data) == 0 || !mediatype.SupportedImage(image.MIMEType) {
					return content{}, false, fmt.Errorf("tool image %d is not resolved", i)
				}
				name := fmt.Sprintf("agentk-tool-image-%d", i+1)
				refs = append(refs, map[string]string{"$ref": name})
				wire.Parts = append(
					wire.Parts,
					functionResponsePart{InlineData: &inlineData{
						MIMEType:    image.MIMEType,
						DisplayName: name,
						Data:        base64.StdEncoding.EncodeToString(image.Data),
					}},
				)
			}
			wire.Response = map[string]any{"result": response, "images": refs}
		}
		return content{Role: "user", Parts: []part{{FunctionResponse: wire}}}, true, nil
	default:
		return content{}, false, fmt.Errorf("unsupported role %q", message.Role)
	}
}

func mediaParts(message chat.Message) ([]part, error) {
	out := make([]part, 0, len(message.Media)+1)
	for i, media := range message.Media {
		mimeType := mediatype.Canonical(media.MIMEType)
		switch mediatype.Kind(mimeType) {
		case mediatype.KindVideo, mediatype.KindAudio:
			if media.URI == "" || len(media.Data) != 0 {
				return nil, fmt.Errorf("%s %d is not a remote reference", mediatype.Kind(mimeType), i)
			}
			out = append(out, part{FileData: &fileData{
				MIMEType: mimeType, FileURI: media.URI,
			}})
		case mediatype.KindImage:
			if len(media.Data) == 0 {
				return nil, fmt.Errorf("image %d is not resolved", i)
			}
			out = append(out, part{InlineData: &inlineData{
				MIMEType: mimeType, Data: base64.StdEncoding.EncodeToString(media.Data),
			}})
		default:
			return nil, fmt.Errorf("media %d has unsupported MIME %q", i, media.MIMEType)
		}
	}
	if message.Text != "" {
		text := message.Text
		out = append(out, part{Text: &text})
	}
	return out, nil
}

func assistantParts(message chat.Message, calls map[string]callMeta) ([]part, error) {
	blocks := message.Blocks
	if len(blocks) == 0 {
		if message.Reasoning != "" {
			blocks = append(blocks, chat.Block{Kind: chat.BlockReasoning, Text: message.Reasoning})
		}
		if message.Text != "" {
			blocks = append(blocks, chat.Block{Kind: chat.BlockText, Text: message.Text})
		}
	}
	out := make([]part, 0, len(blocks)+len(message.ToolCalls))
	for _, block := range blocks {
		text := block.Text
		out = append(
			out,
			part{Text: &text, Thought: block.Kind == chat.BlockReasoning, ThoughtSignature: block.ProtocolState},
		)
	}
	for _, call := range message.ToolCalls {
		var args map[string]any
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return nil, fmt.Errorf("function call %s arguments: %w", call.Name, err)
			}
		}
		calls[call.ID] = callMeta{name: call.Name, providerID: call.ProviderID}
		out = append(
			out,
			part{
				FunctionCall:     &functionCall{ID: call.ProviderID, Name: call.Name, Args: args},
				ThoughtSignature: call.ProtocolState,
			},
		)
	}
	return out, nil
}
