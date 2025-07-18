package mysql

import "encoding/json"

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func fromJSON[T any](data []byte) (*T, error) {
	if data == nil {
		return nil, nil
	}
	var v T
	err := json.Unmarshal(data, &v)
	return &v, err
}

func getOrEmpty[T any](ptr *T) T {
	if ptr != nil {
		return *ptr
	}
	var zero T
	return zero
}
