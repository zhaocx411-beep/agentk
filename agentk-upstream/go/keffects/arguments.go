package keffects

import (
	"errors"
	"fmt"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// ErrOutputMissing reports that a successful QueryResponse does not yet carry a
// named output. keffects can publish success shortly before outputs become
// visible, so consumers may treat this as "poll again" rather than a failure.
var ErrOutputMissing = errors.New("keffects: output is missing")

// ErrInvalidInput marks a typed workflow input that violates the published
// workflow contract. Submitting it would be rejected by keffects, so consumers
// should fail without retrying.
var ErrInvalidInput = errors.New("keffects: invalid workflow input")

func invalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

func StringArgument(name, value string) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_StringValue{StringValue: value}}
}

func BoolArgument(name string, value bool) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_BoolValue{BoolValue: value}}
}

func FloatArgument(name string, value float64) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_FloatValue{FloatValue: value}}
}

func Int64Argument(name string, value int64) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_Int64Value{Int64Value: value}}
}

func StringValuesArgument(name string, values []string) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_StringValues{
		StringValues: &pb.ListString{Value: values},
	}}
}

func MediaArgument(name, bucket, key string) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_MediaValue{
		MediaValue: &pb.Media{Bucket: bucket, Key: key},
	}}
}

func MediaValuesArgument(name string, media []*pb.Media) *pb.Argument {
	return &pb.Argument{Name: name, Value: &pb.Argument_MediaValues{
		MediaValues: &pb.ListMedia{Value: media},
	}}
}

func FindMedia(arguments []*pb.Argument, name string) (*pb.Media, error) {
	argument, err := findArgument(arguments, name)
	if err != nil {
		return nil, err
	}
	media := argument.GetMediaValue()
	if media == nil || media.GetBucket() == "" || media.GetKey() == "" {
		return nil, fmt.Errorf("keffects: output %q is not a valid media value", name)
	}
	return media, nil
}

func FindString(arguments []*pb.Argument, name string) (string, error) {
	argument, err := findArgument(arguments, name)
	if err != nil {
		return "", err
	}
	if _, ok := argument.GetValue().(*pb.Argument_StringValue); !ok {
		return "", fmt.Errorf("keffects: output %q is not a string value", name)
	}
	return argument.GetStringValue(), nil
}

func FindStringValues(arguments []*pb.Argument, name string) ([]string, error) {
	argument, err := findArgument(arguments, name)
	if err != nil {
		return nil, err
	}
	values := argument.GetStringValues()
	if values == nil || len(values.GetValue()) == 0 {
		return nil, fmt.Errorf("keffects: output %q is not a non-empty string array", name)
	}
	return values.GetValue(), nil
}

func FindMediaValues(arguments []*pb.Argument, name string) ([]*pb.Media, error) {
	argument, err := findArgument(arguments, name)
	if err != nil {
		return nil, err
	}
	values := argument.GetMediaValues()
	if values == nil || len(values.GetValue()) == 0 {
		return nil, fmt.Errorf("keffects: output %q is not a non-empty media array", name)
	}
	for index, media := range values.GetValue() {
		if media == nil || media.GetBucket() == "" || media.GetKey() == "" {
			return nil, fmt.Errorf(
				"keffects: output %q contains invalid media at index %d", name, index)
		}
	}
	return values.GetValue(), nil
}

func findArgument(arguments []*pb.Argument, name string) (*pb.Argument, error) {
	for _, argument := range arguments {
		if argument != nil && argument.GetName() == name {
			return argument, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrOutputMissing, name)
}

func optionalMediaValues(arguments []*pb.Argument, name string) []*pb.Media {
	for _, argument := range arguments {
		if argument != nil && argument.GetName() == name && argument.GetMediaValues() != nil {
			return argument.GetMediaValues().GetValue()
		}
	}
	return nil
}

func requireMedia(field string, media *pb.Media) error {
	if media == nil || media.GetBucket() == "" || media.GetKey() == "" {
		return invalidInput("%s requires a staged media with bucket and key", field)
	}
	return nil
}

func requireMediaList(field string, media []*pb.Media, minCount, maxCount int) error {
	if len(media) < minCount || len(media) > maxCount {
		return invalidInput("%s must contain %d to %d media, got %d",
			field, minCount, maxCount, len(media))
	}
	for index, item := range media {
		if err := requireMedia(fmt.Sprintf("%s[%d]", field, index), item); err != nil {
			return err
		}
	}
	return nil
}
