package percentiles

import (
	"strconv"
	"strings"
)

// ConvertToBetterPercentile takes a two-digit percentile value
// and converts it to a three-digit value
func ConvertToBetterPercentile(p int32) int32 {
	if p > 100 {
		return p
	}

	return p * 10
}

// ParsePercentiles takes a string of key-value pairs corresponding to
// percentiles and  values, return a map of them. Converts percentiles
// from two digits to three digits to account for percentiles that
// aren't whole numbers e.g. 99.9
//
// Ex: 50=100,90=200,95=1000,999=20000, we
// return the following map:
// p[500] = 100
// p[900] = 200
// p[950] = 1000
// p[999] = 20000
func ParsePercentiles(input string) (map[int32]int64, error) {
	var percentiles map[int32]int64
	var ss []string

	ss = strings.Split(input, ",")
	percentiles = make(map[int32]int64)
	for _, pair := range ss {
		z := strings.Split(pair, "=")
		percentile, errPercentile := strconv.ParseInt(z[0], 10, 32)
		value, errValue := strconv.ParseInt(z[1], 10, 64)
		if errPercentile == nil && errValue == nil {
			percentiles[ConvertToBetterPercentile(int32(percentile))] = value
		} else {
			if errPercentile != nil {
				return nil, errPercentile
			} else if errValue != nil {
				return nil, errValue
			}
		}
	}

	return percentiles, nil
}
