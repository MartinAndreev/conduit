package startupinfra

import (
	"github.com/MartinAndreev/conduit/internal/lockscope"
	"sync"
)

type capture struct {
	mu       sync.Mutex
	data     []byte
	exceeded bool
}

func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		available := probeCaptureBytes - len(c.data)
		if len(p) > available {
			c.exceeded = true
			p = p[:available]
		}
		c.data = append(c.data, p...)
		return nil
	})
	return n, nil
}
func (c *capture) result() (output string, exceeded bool) {
	_ = lockscope.WithMutexLock(&c.mu, func() error {
		output = string(c.data)
		exceeded = c.exceeded
		return nil
	})
	return
}
