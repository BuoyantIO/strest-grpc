package distribution_test

import (
	"github.com/buoyantio/strest-grpc/client/distribution"
	. "gopkg.in/check.v1"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type DistributionTestSuite struct{}

var _ = Suite(&DistributionTestSuite{})

func (*DistributionTestSuite) TestMapKeysAreSorted(c *C) {
	// Empty distribution has no keys
	empty := distribution.Empty()
	c.Assert(empty.SortedKeys(), DeepEquals, []int{})

	m := map[int]int64{
		200: 200,
		300: 300,
		400: 400,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	// Tests that the elements are the same regardless of sorting
	c.Assert(dist.SortedKeys(), DeepEquals, []int{0, 200, 300, 400, 1000})
	c.Assert(dist.CheckValidity(), IsNil)
}

func (*DistributionTestSuite) TestCheckValueFits(c *C) {
	m := map[int]int64{
		0:    100,
		1000: 1000,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)

	high, low := dist.FindHighLowKeys(200)
	c.Assert(low, Equals, 0)
	c.Assert(high, Equals, 1000)

	c.Assert(dist.CheckKeyValueFits(200, 200), IsNil)
	c.Assert(dist.CheckValidity(), IsNil)

	dist[200] = 200

	high, low = dist.FindHighLowKeys(300)
	c.Assert(low, Equals, 200)
	c.Assert(high, Equals, 1000)

	c.Assert(dist.CheckKeyValueFits(300, 300), IsNil)
	c.Assert(dist.CheckValidity(), IsNil)
}

func (*DistributionTestSuite) TestCheckValueFits2(c *C) {
	m := map[int]int64{
		0:    100,
		1000: 1000,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	dist[200] = 200
	dist[300] = 300

	high, low := dist.FindHighLowKeys(250)
	c.Assert(high, Equals, 300)
	c.Assert(low, Equals, 200)
	c.Assert(dist.CheckKeyValueFits(250, 250), IsNil)
	c.Assert(dist.CheckValidity(), IsNil)
}

func (*DistributionTestSuite) TestFindHighLowKeysLargeValues(c *C) {
	m := map[int]int64{
		0:    0,
		1000: 1100,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	c.Assert(len(dist), Equals, 2)

	c.Assert(dist.SortedKeys(), DeepEquals, []int{0, 1000})
	high, low := dist.FindHighLowKeys(500)

	if low > high {
		c.Errorf("%d > %d", low, high)
	}

	c.Assert(low, Equals, 0)
	c.Assert(dist[low], Equals, int64(0))

	c.Assert(high, Equals, 1000)
	c.Assert(dist[high], Equals, int64(1100))
	c.Assert(dist.CheckValidity(), IsNil)
}

func (*DistributionTestSuite) TestFindHighLowKeysSmallValues(c *C) {
	m := map[int]int64{
		500: 100,
		900: 200,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	c.Assert(dist, Not(IsNil))
	high, low := dist.FindHighLowKeys(750)
	c.Assert(high, Equals, 900)
	c.Assert(low, Equals, 500)

	high, low = dist.FindHighLowKeys(499)
	c.Assert(high, Equals, 500)
	c.Assert(low, Equals, 0)

	high, low = dist.FindHighLowKeys(999)
	c.Assert(high, Equals, 1000)
	c.Assert(low, Equals, 900)
}

func (*DistributionTestSuite) TestEmpty(c *C) {
	dist := distribution.Empty()
	c.Check(dist, Not(IsNil))
	c.Check(len(dist), Equals, 0)
}

func (*DistributionTestSuite) TestCheckKeyValueFits(c *C) {
	m := map[int]int64{
		1000: 30,
	}

	dist0_30, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	c.Check(len(dist0_30), Equals, 2)
	c.Check(dist0_30[0], Equals, int64(0))
	c.Check(dist0_30[1000], Equals, int64(30))

	c.Check(dist0_30.CheckKeyValueFits(900, 40000), Not(IsNil))
}

func (*DistributionTestSuite) TestMapApi(c *C) {
	m := map[int]int64{
		0:    0,
		1000: 500,
	}
	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	dist[200] = 200
	dist[300] = 300
	dist[400] = 400

	c.Check(dist.CheckValidity(), IsNil)

	dist = distribution.Empty()
	dist[200] = 200
	dist[300] = 300
	dist[400] = 400
	dist.AddMinMax()

	c.Check(dist.CheckValidity(), IsNil)
}

func (*DistributionTestSuite) TestMapApiErrors(c *C) {
	m := map[int]int64{
		0:    0,
		1000: 10,
	}
	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	dist[500] = 20

	c.Check(dist.CheckValidity(), Not(IsNil))
}

func (*DistributionTestSuite) TestExtraploateFromMap(c *C) {
	m := map[int]int64{
		0:    100,
		200:  2000,
		1000: 10000,
	}
	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	c.Assert(dist, Not(IsNil))
	c.Assert(dist.Get(0), Equals, int64(0))
	c.Assert(dist.Get(200), Equals, int64(2000))
	c.Assert(dist.Get(500), Equals, int64(5000))
	c.Assert(dist.Get(990), Equals, int64(9900))
	c.Assert(dist.Get(1000), Equals, int64(10000))
}

func (*DistributionTestSuite) TestFromMapGood(c *C) {
	latencyMap := map[int]int64{
		10:  1,
		500: 1000,
		900: 2000,
		950: 4000,
		999: 10000,
	}

	dist, err := distribution.FromMap(latencyMap)
	c.Assert(err, IsNil)
	// We correctly add a min to the distribution
	c.Assert(dist.Get(0), Equals, int64(0))
	c.Assert(dist.Get(500), Equals, int64(1000))

	c.Assert(dist.Get(700), Equals, int64(1500))
	c.Assert(dist.Get(900), Equals, int64(2000))
	c.Assert(dist.Get(975), Equals, int64(6448))

	// We correctly add a max to the distribution
	c.Assert(dist.Get(1000), Equals, int64(10000))
}

func (*DistributionTestSuite) TestFromMapBad(c *C) {
	m := map[int]int64{
		0:   0,
		500: 200,
		// 100 doesn't fit the distribution.
		750: 100,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(dist, IsNil)
	c.Assert(err, Not(IsNil))
}

func (*DistributionTestSuite) TestAddMinMax(c *C) {
	m := map[int]int64{
		100: 100,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)

	c.Assert(dist[0], Equals, int64(0))
	c.Assert(dist[100], Equals, int64(100))
	c.Assert(dist[1000], Equals, int64(100))
}

func (*DistributionTestSuite) TestSmallValues(c *C) {
	m := map[int]int64{
		0:    0,
		500:  50,
		1000: 100,
	}

	dist, err := distribution.FromMap(m)
	c.Assert(err, IsNil)
	c.Assert(dist, Not(IsNil))
	c.Assert(dist.Get(0), Equals, int64(0))

	c.Assert(dist.Get(250), Equals, int64(25))
	high, low := dist.FindHighLowKeys(400)
	c.Assert(high, Equals, 500)
	c.Assert(dist[500], Equals, int64(50))
	c.Assert(low, Equals, 0)
	c.Assert(dist[0], Equals, int64(0))
	c.Assert(dist.Get(400), Equals, int64(40))
	c.Assert(dist.Get(500), Equals, int64(50))

	c.Assert(dist.Get(750), Equals, int64(75))
	c.Assert(dist.Get(1000), Equals, int64(100))
}
