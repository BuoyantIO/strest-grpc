package percentiles_test

import (
	"testing"

	"github.com/buoyantio/strest-grpc/percentiles"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type PercentilesTestSuite struct{}

var _ = Suite(&PercentilesTestSuite{})

func (*PercentilesTestSuite) TestGoodMap(c *C) {
	input := "50=10,99=100,999=200,100=1000"
	percents, err := percentiles.ParsePercentiles(input)
	c.Assert(percents, Not(IsNil))
	c.Assert(err, IsNil)

	c.Assert(percents[500], Not(IsNil))
	c.Assert(percents[500], Equals, int64(10))
	c.Assert(percents[50], Equals, int64(0))

	// 95 was not provided so will not be in the map
	c.Assert(percents[950], Equals, int64(0))
	c.Assert(percents[95], Equals, int64(0))
}

func (*PercentilesTestSuite) TestSmallKeys(c *C) {
	input := "10=10,50=50"
	percents, err := percentiles.ParsePercentiles(input)
	c.Assert(err, IsNil)
	c.Assert(percents, Not(IsNil))
	c.Assert(percents[100], Equals, int64(10))
	c.Assert(percents[500], Equals, int64(50))
}

func (*PercentilesTestSuite) TestBadKey(c *C) {
	input := "50=10,99=100,999x=200,100=1000"
	percents, err := percentiles.ParsePercentiles(input)
	c.Assert(percents, IsNil)
	c.Assert(err, Not(IsNil))

	input = "x50=10,99=100,999x=200,100=1000"
	percents, err = percentiles.ParsePercentiles(input)
	c.Assert(percents, IsNil)
	c.Assert(err, Not(IsNil))
}

func (*PercentilesTestSuite) TestBadValues(c *C) {
	input := "50=10,99=100,999=200x,100=1000"
	percents, err := percentiles.ParsePercentiles(input)
	c.Assert(percents, IsNil)
	c.Assert(err, Not(IsNil))

	input = "50=10,99=100,999x=200,100=1000x"
	percents, err = percentiles.ParsePercentiles(input)
	c.Assert(percents, IsNil)
	c.Assert(err, Not(IsNil))
}
