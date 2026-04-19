package coding

import (
	"errors"

	"github.com/leonkaihao/cache/pkg/model"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type jsonpbCoder struct {
	marshaller   *protojson.MarshalOptions
	unmarshaller *protojson.UnmarshalOptions
}

func NewJsonpbCoder() model.Coder {
	return &jsonpbCoder{
		marshaller:   &protojson.MarshalOptions{},
		unmarshaller: &protojson.UnmarshalOptions{},
	}
}

func (jc *jsonpbCoder) Encode(data any) (string, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return "", errors.New("data is not a proto.Message")
	}
	bytes, err := jc.marshaller.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (jc *jsonpbCoder) Decode(data string, out any) error {
	msg, ok := out.(proto.Message)
	if !ok {
		return errors.New("out is not a proto.Message")
	}
	return jc.unmarshaller.Unmarshal([]byte(data), msg)
}
