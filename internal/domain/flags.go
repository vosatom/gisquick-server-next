package domain

type Flags []string
type StringArray = Flags

func contains(items []string, value string) bool {
	for _, i := range items {
		if i == value {
			return true
		}
	}
	return false
}

func (f Flags) Has(flag string) bool {
	return contains(f, flag)
}

func (f Flags) Union(flags Flags) Flags {
	m := make(map[string]bool)
	for _, item := range f {
		m[item] = true
	}
	for _, item := range flags {
		_, exists := m[item]
		if !exists {
			f = append(f, item)
		}
	}
	return f
}

func (f Flags) Intersection(flags Flags) Flags {
	m := make(map[string]bool)
	for _, item := range f {
		m[item] = true
	}
	res := []string{}
	for _, item := range flags {
		_, exists := m[item]
		if exists {
			res = append(res, item)
		}
	}
	return res
}

func (f Flags) Clone() Flags {
	copy := make(Flags, len(f))
	for i, f := range f {
		copy[i] = f
	}
	return copy
}

func (f Flags) Filter(test func(item string) bool) []string {
	res := make([]string, 0)
	for _, v := range f {
		if test(v) {
			res = append(res, v)
		}
	}
	return res
}
