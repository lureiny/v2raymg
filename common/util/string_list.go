package util

type StringList []string

type filterUriFunc func(string) bool

// 保留返回true的元素
func (s StringList) Filter(f filterUriFunc) StringList {
	result := StringList{}
	for _, ss := range s {
		if f(ss) {
			result = append(result, ss)
		}
	}
	return result
}

func (s StringList) Contains(sub string) bool {
	for _, ss := range s {
		if ss == sub {
			return true
		}
	}
	return false
}
