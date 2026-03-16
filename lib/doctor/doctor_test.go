package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncatedJoin(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	assert.Equal(t, "a\nb\nc\nd\ne", truncatedJoin(items, 0), "maxItems=0 returns all")
	assert.Equal(t, "a\nb\nc\nd\ne", truncatedJoin(items, 5), "exact count returns all")
	assert.Equal(t, "a\nb\nc\nd\ne", truncatedJoin(items, 10), "over count returns all")
	assert.Equal(t, "a\nb\n... and 3 more", truncatedJoin(items, 2), "truncates with summary")
}
