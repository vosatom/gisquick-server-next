package maps

func NewMap(data map[string]interface{}) map[string]interface{} {
	res := make(map[string]interface{}, len(data))
	for k, v := range data {
		res[k] = v
	}
	return res
}
