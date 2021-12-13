package common

type StringList []string

type filterUriFunc func(string) bool

// 保留返回true的元素
func (s StringList) Filter(f filterUriFunc) StringList {
	newUris := StringList{}
	for _, uri := range s {
		if f(uri) {
			newUris = append(newUris, uri)
		}
	}
	return newUris
}
