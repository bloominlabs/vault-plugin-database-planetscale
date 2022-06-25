package planetscale

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/vault/sdk/database/helper/connutil"
	"github.com/mitchellh/mapstructure"
	"github.com/planetscale/planetscale-go/planetscale"
)

type ConnectionParameters struct {
	Organization string `json:"organization" structs:"organization" mapstructure:"organization"`
	Database     string `json:"database" structs:"database" mapstructure:"database"`
}

// planetscaleConnectionProducer implements ConnectionProducer and provides an
// interface for databases to make connections.
type planetscaleConnectionProducer struct {
	Organization string `json:"organization" structs:"organization" mapstructure:"organization"`
	Database     string `json:"database" structs:"database" mapstructure:"database"`
	ServiceToken string `json:"service_token" structs:"service_token" mapstructure:"service_token"`
	TokenName    string `json:"token_name" structs:"token_name" mapstructure:"token_name"`

	Initialized bool
	RawConfig   map[string]interface{}
	Type        string
	client      *planetscale.Client
	sync.Mutex
}

func (c *planetscaleConnectionProducer) Init(ctx context.Context, conf map[string]interface{}, verifyConnection bool) (map[string]interface{}, error) {
	c.Lock()
	defer c.Unlock()

	c.RawConfig = conf

	err := mapstructure.WeakDecode(conf, &c)
	if err != nil {
		return nil, err
	}

	if c.Organization == "" {
		return nil, fmt.Errorf("organization cannot be empty")
	}
	if c.Database == "" {
		return nil, fmt.Errorf("database cannot be empty")
	}
	if c.ServiceToken == "" {
		return nil, fmt.Errorf("service_token cannot be empty")
	}
	if c.TokenName == "" {
		return nil, fmt.Errorf("token_name cannot be empty")
	}

	client, err := c.createClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create planetscale client: %w", err)
	}
	c.client = client

	// Set initialized to true at this point since all fields are set,
	// and the connection can be established at a later time.
	c.Initialized = true

	return c.RawConfig, nil
}

// Connection creates or returns an existing a database connection. If the session fails
// on a ping check, the session will be closed and then re-created.
// This method does locks the mutex on its own.
func (c *planetscaleConnectionProducer) Connection(ctx context.Context) (*planetscale.Client, error) {
	if !c.Initialized {
		return nil, connutil.ErrNotInitialized
	}

	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if c.client != nil {
		return c.client, nil
	}

	client, err := c.createClient(ctx)
	if err != nil {
		return nil, err
	}
	c.client = client
	return c.client, nil
}

func (c *planetscaleConnectionProducer) createClient(ctx context.Context) (client *planetscale.Client, err error) {
	client, err = planetscale.NewClient(
		planetscale.WithServiceToken(c.TokenName, c.ServiceToken),
	)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Close terminates the database connection.
func (c *planetscaleConnectionProducer) Close() error {
	c.Lock()
	defer c.Unlock()

	c.client = nil

	return nil
}

func (c *planetscaleConnectionProducer) secretValues() map[string]string {
	return map[string]string{
		c.ServiceToken: "[ServiceToken]",
	}
}
