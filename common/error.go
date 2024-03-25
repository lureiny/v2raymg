package common

import (
	"fmt"
)

func MergeError(srcError, newError error) error {
	if srcError == nil {
		return newError
	}
	if newError == nil {
		return srcError
	}
	return fmt.Errorf("%v%s%v", srcError, ErrMsgSplitSign, newError)
}
