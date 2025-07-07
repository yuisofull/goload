package rediscache

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
)

type Marshaler[V any] interface {
	Marshal(v V) ([]byte, error)
	Unmarshal(data []byte) (V, error)
}

type DefaultMarshaler[V any] struct {
	GobMarshaler[V]
}

type GobMarshaler[V any] struct{}

func (GobMarshaler[V]) Marshal(v V) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(v)
	return buf.Bytes(), err
}

func (GobMarshaler[V]) Unmarshal(data []byte) (V, error) {
	var v V
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&v)
	return v, err
}

type JSONMarshaler[V any] struct{}

func (j JSONMarshaler[V]) Marshal(v V) ([]byte, error) {
	return json.Marshal(v)
}

func (j JSONMarshaler[V]) Unmarshal(data []byte) (V, error) {
	var v V
	err := json.Unmarshal(data, &v)
	return v, err
}
