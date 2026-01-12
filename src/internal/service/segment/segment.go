package segment

import (
	"fmt"
	"sync/atomic"
)

type Generator struct {
	counter uint64
}

func New() *Generator {
	return &Generator{}
}

func (g *Generator) Next(interactionId string) string {
	n := atomic.AddUint64(&g.counter, 1)
	return fmt.Sprintf("%s-seg-%d", interactionId, n)
}
