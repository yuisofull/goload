package rediscache

import "fmt"

type KeyEncoder[K comparable] interface {
	Encode(k K) (string, error)
	Namespace() string
}

type DefaultKeyEncoder[K comparable] struct{}

func (DefaultKeyEncoder[K]) Encode(k K) (string, error) {
	return fmt.Sprintf("%T:%v", k, k), nil
}

func (DefaultKeyEncoder[K]) Namespace() string {
	return ""
}

type PrefixKeyEncoder[K comparable] struct {
	Prefix string
	Inner  KeyEncoder[K]
}

func (p PrefixKeyEncoder[K]) Encode(k K) (string, error) {
	enc, err := p.Inner.Encode(k)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%s", p.Prefix, enc), nil
}

func (p PrefixKeyEncoder[K]) Namespace() string {
	return p.Prefix
}
