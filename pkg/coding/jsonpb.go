package coding

import (
	"errors"

	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/protobuf/runtime/protoiface"
	"github.com/leonkaihao/cache/pkg/model"
)

type jsonpbCoder struct {
	marshaller *jsonpb.Marshaler
}

func NewJsonpbCoder() model.Coder {
	return &jsonpbCoder{
		marshaller: &jsonpb.Marshaler{},
	}
}

func (jc *jsonpbCoder) Encode(data any) (string, error) {
	return jc.marshaller.MarshalToString(data.(protoiface.MessageV1))
}

func (jc *jsonpbCoder) Decode(data string, out any) error {
	switch result := out.(type) {
	case protoiface.MessageV1:
		return jsonpb.UnmarshalString(data, result)
	default:
		return errors.New("unknown type for decoding")
	}
}
