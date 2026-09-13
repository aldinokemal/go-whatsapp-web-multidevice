package utils

import "encoding/json"

// Nullable carries the three states a JSON field can be in on a PATCH body:
// absent (Set false), explicit null (Set true, Valid false) and an actual value
// (Set true, Valid true). A plain pointer collapses the first two, which leaves
// callers unable to reset a stored override back to NULL.
type Nullable[T any] struct {
	Set   bool
	Valid bool
	Value T
}

func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
		var zero T
		n.Valid = false
		n.Value = zero
		return nil
	}
	if err := json.Unmarshal(data, &n.Value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}

// Ptr returns nil for both the absent and the explicit-null state; use Set to
// tell them apart before deciding whether to write the nil.
func (n Nullable[T]) Ptr() *T {
	if !n.Valid {
		return nil
	}
	value := n.Value
	return &value
}
