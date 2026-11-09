package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	inferpb "git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/gen/infer"
)

func decodeFeatures(input map[string]json.RawMessage) (map[string]*inferpb.ExFeature, error) {
	output := make(map[string]*inferpb.ExFeature, len(input))
	for name, raw := range input {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("feature %q: %w", name, err)
		}
		if err := expectEOF(decoder); err != nil {
			return nil, fmt.Errorf("feature %q: %w", name, err)
		}
		feature, err := featureFromJSON(value)
		if err != nil {
			return nil, fmt.Errorf("feature %q: %w", name, err)
		}
		feature.Name = name
		output[name] = feature
	}
	return output, nil
}

func featureFromJSON(value any) (*inferpb.ExFeature, error) {
	switch typed := value.(type) {
	case string:
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_StrVal{StrVal: typed}}, nil
	case bool:
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_BoolVal{BoolVal: typed}}, nil
	case json.Number:
		return numberFeature(typed)
	case []any:
		return arrayFeature(typed)
	case map[string]any:
		return mapFeature(typed)
	case nil:
		return nil, errors.New("null is not a valid ExFeature value")
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func numberFeature(number json.Number) (*inferpb.ExFeature, error) {
	if !strings.ContainsAny(number.String(), ".eE") {
		value, err := number.Int64()
		if err == nil {
			return &inferpb.ExFeature{Kind: &inferpb.ExFeature_IntVal{IntVal: value}}, nil
		}
	}
	value, err := number.Float64()
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || value > math.MaxFloat32 || value < -math.MaxFloat32 {
		return nil, fmt.Errorf("number %q is outside the supported range", number)
	}
	return &inferpb.ExFeature{Kind: &inferpb.ExFeature_FloatVal{FloatVal: float32(value)}}, nil
}

func arrayFeature(values []any) (*inferpb.ExFeature, error) {
	if strings, ok := allStrings(values); ok {
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_StrArray{StrArray: &inferpb.StringArray{StrElems: strings}}}, nil
	}
	if booleans, ok := allBools(values); ok {
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_BoolArray{BoolArray: &inferpb.BoolArray{BoolElems: booleans}}}, nil
	}
	if integers, ok := allInts(values); ok {
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_IntArray{IntArray: &inferpb.Int64Array{Int64Elems: integers}}}, nil
	}
	if floats, ok, err := allFloats(values); err != nil {
		return nil, err
	} else if ok {
		return &inferpb.ExFeature{Kind: &inferpb.ExFeature_FloatArray{FloatArray: &inferpb.FloatArray{FloatElems: floats}}}, nil
	}

	items := make([]*inferpb.ExFeature, 0, len(values))
	for index, value := range values {
		item, err := featureFromJSON(value)
		if err != nil {
			return nil, fmt.Errorf("array element %d: %w", index, err)
		}
		items = append(items, item)
	}
	return &inferpb.ExFeature{Kind: &inferpb.ExFeature_ArrayVal{ArrayVal: &inferpb.ExFeatureArray{ArrayElems: items}}}, nil
}

func mapFeature(values map[string]any) (*inferpb.ExFeature, error) {
	if len(values) == 1 {
		if raw, ok := values["$bytes"]; ok {
			encoded, ok := raw.(string)
			if !ok {
				return nil, errors.New(`"$bytes" must be a base64 string`)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf(`decode "$bytes": %w`, err)
			}
			return &inferpb.ExFeature{Kind: &inferpb.ExFeature_BytesVal{BytesVal: decoded}}, nil
		}
	}

	items := make(map[string]*inferpb.ExFeature, len(values))
	for name, value := range values {
		item, err := featureFromJSON(value)
		if err != nil {
			return nil, fmt.Errorf("map key %q: %w", name, err)
		}
		item.Name = name
		items[name] = item
	}
	return &inferpb.ExFeature{Kind: &inferpb.ExFeature_MapVal{MapVal: &inferpb.ExFeatureMap{Map: items}}}, nil
}

func encodeFeatures(input map[string]*inferpb.ExFeature) map[string]any {
	output := make(map[string]any, len(input))
	for name, feature := range input {
		output[name] = featureToJSON(feature)
	}
	return output
}

func featureToJSON(feature *inferpb.ExFeature) any {
	if feature == nil {
		return nil
	}
	switch value := feature.Kind.(type) {
	case *inferpb.ExFeature_IntVal:
		return value.IntVal
	case *inferpb.ExFeature_Int32Val:
		return value.Int32Val
	case *inferpb.ExFeature_FloatVal:
		return value.FloatVal
	case *inferpb.ExFeature_StrVal:
		return value.StrVal
	case *inferpb.ExFeature_BytesVal:
		return map[string]any{"$bytes": base64.StdEncoding.EncodeToString(value.BytesVal)}
	case *inferpb.ExFeature_BoolVal:
		return value.BoolVal
	case *inferpb.ExFeature_IntArray:
		return value.IntArray.GetInt64Elems()
	case *inferpb.ExFeature_Int32Array:
		return value.Int32Array.GetInt32Elems()
	case *inferpb.ExFeature_FloatArray:
		return value.FloatArray.GetFloatElems()
	case *inferpb.ExFeature_StrArray:
		return value.StrArray.GetStrElems()
	case *inferpb.ExFeature_BytesArray:
		encoded := make([]map[string]any, 0, len(value.BytesArray.GetBytesElems()))
		for _, item := range value.BytesArray.GetBytesElems() {
			encoded = append(encoded, map[string]any{"$bytes": base64.StdEncoding.EncodeToString(item)})
		}
		return encoded
	case *inferpb.ExFeature_BoolArray:
		return value.BoolArray.GetBoolElems()
	case *inferpb.ExFeature_MapVal:
		return encodeFeatures(value.MapVal.GetMap())
	case *inferpb.ExFeature_ArrayVal:
		items := make([]any, 0, len(value.ArrayVal.GetArrayElems()))
		for _, item := range value.ArrayVal.GetArrayElems() {
			items = append(items, featureToJSON(item))
		}
		return items
	case *inferpb.ExFeature_MediaData:
		return mediaToJSON(value.MediaData)
	case *inferpb.ExFeature_MediaDataArray:
		items := make([]any, 0, len(value.MediaDataArray.GetMediaDataElems()))
		for _, item := range value.MediaDataArray.GetMediaDataElems() {
			items = append(items, mediaToJSON(item))
		}
		return items
	default:
		return nil
	}
}

func mediaToJSON(media *inferpb.MediaData) any {
	if media == nil {
		return nil
	}
	key := media.GetBlobstoreKey()
	return map[string]any{
		"blobstoreKey": map[string]any{
			"db": key.GetDb(), "table": key.GetTable(), "key": key.GetKey(),
			"bucketName": key.GetBucketName(), "customRequestHeader": key.GetCustomRequestHeader(),
		},
		"extraParams": media.GetExtraParams(),
	}
}

func allStrings(values []any) ([]string, bool) {
	if len(values) == 0 {
		return nil, false
	}
	result := make([]string, len(values))
	for index, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		result[index] = text
	}
	return result, true
}

func allBools(values []any) ([]bool, bool) {
	if len(values) == 0 {
		return nil, false
	}
	result := make([]bool, len(values))
	for index, value := range values {
		boolean, ok := value.(bool)
		if !ok {
			return nil, false
		}
		result[index] = boolean
	}
	return result, true
}

func allInts(values []any) ([]int64, bool) {
	if len(values) == 0 {
		return nil, false
	}
	result := make([]int64, len(values))
	for index, value := range values {
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(number.String(), ".eE") {
			return nil, false
		}
		integer, err := number.Int64()
		if err != nil {
			return nil, false
		}
		result[index] = integer
	}
	return result, true
}

func allFloats(values []any) ([]float32, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	result := make([]float32, len(values))
	for index, value := range values {
		number, ok := value.(json.Number)
		if !ok {
			return nil, false, nil
		}
		floating, err := number.Float64()
		if err != nil || math.IsInf(floating, 0) || math.IsNaN(floating) || floating > math.MaxFloat32 || floating < -math.MaxFloat32 {
			return nil, false, fmt.Errorf("array element %d is outside the supported float range", index)
		}
		result[index] = float32(floating)
	}
	return result, true, nil
}

func expectEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("must contain exactly one JSON value")
		}
		return err
	}
	return nil
}
