package problem

import "encoding/json"

// errorsMember is the extension member that holds the validation items of RFC
// 9457 §3.
const errorsMember = "errors"

// Item is one error of a validation problem (RFC 9457 §3): an explanation for
// the client and, optionally, a JSON Pointer to the member of the request that
// caused it. Build the pointer with [Pointer].
type Item struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer,omitempty"`
}

// WithErrors sets the "errors" extension member to items, in order: a list of
// objects with a "detail" member, always written, and a "pointer" member,
// written when it is not empty. With no items, the member is an empty list.
// Pointers are written as given: build them with [Pointer].
//
// WithErrors owns the "errors" member. A key named "errors", declared with
// [NewKey], sets the same member: whichever option is applied last wins.
func WithErrors(items ...Item) Option {
	if items == nil {
		items = []Item{}
	}
	return WithExtension(Key[[]Item]{name: errorsMember}, items)
}

// Errors returns the items of the "errors" extension member of e, in order,
// and true. It returns nil and false when e is nil, when the member is absent,
// and when its value is not an array of objects whose "detail" and "pointer"
// members, when present, are strings. Members are matched by their exact names.
// Other members of the items are ignored, and [Error.Extensions] still returns
// them.
func (e *Error) Errors() ([]Item, bool) {
	if e == nil {
		return nil, false
	}
	raw, ok := e.ext[errorsMember]
	if !ok {
		return nil, false
	}
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objects); err != nil || objects == nil {
		return nil, false
	}
	items := make([]Item, len(objects))
	for i, obj := range objects {
		if obj == nil {
			return nil, false
		}
		detail, okDetail := optionalString(obj, "detail")
		pointer, okPointer := optionalString(obj, "pointer")
		if !okDetail || !okPointer {
			return nil, false
		}
		items[i] = Item{Detail: detail, Pointer: pointer}
	}
	return items, true
}

// optionalString returns the member name of obj when it is a JSON string, and
// "" when it is absent. It returns false when the member has another type.
func optionalString(obj map[string]json.RawMessage, name string) (string, bool) {
	if _, ok := obj[name]; !ok {
		return "", true
	}
	return stringMember(obj, name)
}
