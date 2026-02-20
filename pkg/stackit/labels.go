package stackit

// ToLabels converts a usual labels map to a type that the SDK accepts.
func ToLabels(labels map[string]string) map[string]any {
	out := make(map[string]any, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

type LabelSelector map[string]string

// Matches reports whether the labels of an SDK resource have all labels of this selector. I.e., additional labels on
// the resource are ignored.
func (s LabelSelector) Matches(labels map[string]any) bool {
	for k, v := range s {
		value, ok := labels[k]
		if !ok {
			return false
		}
		stringValue, ok := value.(string)
		if !ok || stringValue != v {
			return false
		}
	}
	return true
}
