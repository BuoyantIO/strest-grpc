package distribution

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

/**
 * Distribution is a map of percentiles (multiplied by 10) to values.
 * e.g. a latency distribution might be
 * 500  -> 1
 * 900  -> 20
 * 950  -> 30
 * 990  -> 50
 * 999  -> 100
 * 1000 -> 100
 *
 * When a value is requested that no exact value is known for, we'll
 * calculate it's value as a linear extrapolation.
 */
type Distribution map[int]int64

/**
 * Ensure that keys and values are in sorted order.
 */
func (dist *Distribution) CheckValidity() error {
	lastSeenKey := 0
	lastSeenValue := int64(0)

	for _, key := range dist.SortedKeys() {
		if key >= lastSeenKey && (*dist)[key] >= lastSeenValue {
			lastSeenKey = key
			lastSeenValue = (*dist)[key]
		} else {
			return fmt.Errorf("%d >= %d and %d >= %d did not hold for %s", key, lastSeenKey, (*dist)[key], lastSeenValue, *dist)
		}
	}

	return nil
}

/**
 * Ensures that a key and value will fit properly into a distribution
 * that doesn't already contain them.
 * If the distribution already contains the key/value pair, then
 * return immediately.
 */
func (dist *Distribution) CheckKeyValueFits(key int, value int64) error {
	if (*dist)[key] == value {
		return nil
	}

	high, low := dist.FindHighLowKeys(key)

	if key > 1000 {
		return errors.New("key is larger than the largest possible percentile")
	}

	if key < 0 {
		return errors.New("key is smaller than smallest exiting key")
	}

	if value > (*dist)[high] {
		return errors.New("value is larger than next largest value and thus doensn't fit the distribution")
	}

	if value < (*dist)[low] {
		return errors.New("value is smaller than previous smallest value and thus doesn't fit the distribution")
	}

	return nil
}

/**
 * Returns the keys in the distribution sorted numerically.
 */
func (dist *Distribution) SortedKeys() []int {
	var keys = []int{}
	for key, _ := range *dist {
		keys = append(keys, key)
	}

	sort.Ints(keys)

	return keys
}

/**
 * Returns a key pair that is directly above and below the requested
 * key.
 *
 * Precondition: the Distribution must have hadd AddMinMax() called
 * and pass a CheckValidity() test
 */
func (dist *Distribution) FindHighLowKeys(key int) (int, int) {
	low := 0
	high := 0

	keys := dist.SortedKeys()

	for i := range keys {
		if key < keys[i] {
			high = keys[i]
			// Once you're found high, the low key is directly below it.
			// This is why there's a precondition that you've called
			// AddMinMax(), it'll ensure your Distribution has at least two keys.
			low = keys[i-1]
			break
		}
	}

	return high, low
}

/**
 * If the key doesn't exist, returns a linear extrapolation based on
 * its position.
 */
func (dist *Distribution) Get(untrustedKey int) int64 {
	// Ensures that the key is in the range [0,1000]
	requested := int(math.Min(math.Max(float64(untrustedKey), 0), 1000))
	if value, ok := (*dist)[requested]; ok {
		return value
	} else {
		high, low := dist.FindHighLowKeys(requested)
		highValue := (*dist)[high]
		lowValue := (*dist)[low]

		// Reminder: percentiles are multiplied by 10, values are not.
		increment := float64(highValue-lowValue) / (float64(high-low) / float64(10))
		delta := float64((requested-low)/10) * increment

		return lowValue + int64(delta)
	}
}

/**
 * Ensures that the Distribution's min/max are set properly.
 */
func (dist *Distribution) AddMinMax() {
	// Always set min to 0.
	(*dist)[0] = 0

	// Set max to the current maximum value if no max is set
	if _, ok := (*dist)[1000]; !ok {
		max := int64(0)
		for _, key := range (*dist).SortedKeys() {
			value := (*dist)[key]
			if value > max {
				max = value
			}
		}
		(*dist)[1000] = max
	}
}

/**
 * Given a map of three-digit percentiles to int64 values, returns
 * a validated Distribution.
 *
 * This function expects the percentiles to be mapped from the
 * two-digit space into the three-digit space. so instead of 99.9
 * you'd pass in 999.
 */
func FromMap(m map[int]int64) (Distribution, error) {
	dist := Empty()

	for key, value := range m {
		dist[key] = value
	}

	dist.AddMinMax()

	if err := dist.CheckValidity(); err == nil {
		return dist, nil
	} else {
		return nil, err
	}
}

func Empty() Distribution {
	return Distribution{}
}
