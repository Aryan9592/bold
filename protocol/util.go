package protocol

import (
	"math"
	"math/bits"
)

func bisectionPoint(pre, post uint64) (uint64, error) {
	if pre+2 > post {
		return 0, ErrInvalid
	}
	if pre+2 == post {
		return pre + 1, nil
	}
	matchingBits := bits.LeadingZeros64((post - 1) ^ pre)
	mask := uint64(math.MaxUint64) << (63 - matchingBits)
	return (post - 1) & mask, nil
}
