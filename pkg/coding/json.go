package coding

import (
	"encoding/json"

	"github.com/leonkaihao/cache/pkg/model"
)

type jsonCoder struct {
}

func NewJsonCoder() model.Coder {
	return &jsonCoder{}
}

func (cd *jsonCoder) Encode(data any) (string, error) {
	result, err := json.Marshal(data)
	if err != nil {
		result = []byte{}
	}
	return string(result), err
}

func (cd *jsonCoder) Decode(data string, out any) error {
	return json.Unmarshal([]byte(data), out)
}
