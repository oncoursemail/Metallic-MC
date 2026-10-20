package control

import (
	"errors"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/hosting"
)

// ConfigureHostingStore is a startup operation, before Handler serves requests.
// An invalid, unavailable or already-open store must stop startup; silently
// falling back to memory would discard the owner's durable revocations.
func (c *Controller) ConfigureHostingStore(path string) error {
	if !c.cfg.ArchitectEnabled {
		return errors.New("Architect is disabled")
	}
	if c.hosts != nil && c.hosts.Persistent() {
		return errors.New("hosting store already configured")
	}
	options := hosting.DefaultConfig()
	options.Now = func() time.Time { return c.now() }
	service, err := hosting.NewPersistent(options, path)
	if err != nil {
		return err
	}
	c.hosts = service
	return nil
}

// Close releases the private hosting-store lock after serving requests stops.
// Sessions and telemetry intentionally remain ephemeral. A restarted controller
// has no online nodes until fresh authenticated agent heartbeats arrive.
func (c *Controller) Close() error {
	if c.hosts != nil {
		return c.hosts.Close()
	}
	return nil
}
