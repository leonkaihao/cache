package model

// Encoder and Decoder interface for serialized data, especially for centralized cache store(redis values)

type Encoder interface {
	Encode(data any) (string, error)
}

type Decoder interface {
	Decode(data string, out any) error
}

type Coder interface {
	Encoder
	Decoder
}
