package loadtest

import (
	"encoding"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest/clients"
	"github.com/interchainio/tm-load-test/pkg/timeutils"

	"github.com/BurntSushi/toml"
)

// Environment variable-related constants.
const (
	EnvPrefix            = "TMLOADTEST_"
	EnvOutageSimUser     = EnvPrefix + "OUTAGESIMUSER"
	EnvOutageSimPassword = EnvPrefix + "OUTAGESIMPASS"
)

// DefaultHealthCheckInterval is the interval at which slave nodes are expected
// to send a `LoadTestUnderway` message after starting their load testing.
const DefaultHealthCheckInterval = 2 * time.Second

// DefaultMaxMissedHealthChecks is the number of health checks that a slave can
// miss before being considered as "failed" (i.e. one more missed health check
// than `DefaultMaxMissedHealthChecks` will result in total load testing
// failure). Also applies to the slaves' attempts to reach the master before
// they consider the master to be down.
const DefaultMaxMissedHealthChecks = 2

// DefaultMaxMissedHealthCheckPeriod is the time after which a slave is
// considered to have failed if we don't hear from it during that period.
const DefaultMaxMissedHealthCheckPeriod = ((DefaultMaxMissedHealthChecks + 1) * DefaultHealthCheckInterval)

// Config is the central configuration structure for our load testing, from both
// the master and slaves' perspectives.
type Config struct {
	Master      MasterConfig      `toml:"master"`       // The master's load testing configuration.
	Slave       SlaveConfig       `toml:"slave"`        // The slaves' load testing configuration.
	TestNetwork TestNetworkConfig `toml:"test_network"` // The test network layout/configuration.
	Clients     clients.Config    `toml:"clients"`      // Load testing client-related configuration.
}

// MasterConfig provides the configuration for the load testing master.
type MasterConfig struct {
	Bind               string                      `toml:"bind"`                          // The address to which to bind the master (host:port).
	Auth               MasterAuthConfig            `toml:"auth"`                          // Authentication configuration for the master.
	ExpectSlaves       int                         `toml:"expect_slaves"`                 // The number of slaves to expect to connect before starting the load test.
	ExpectSlavesWithin timeutils.ParseableDuration `toml:"expect_slaves_within"`          // The time period within which to expect to hear from all slaves, otherwise causes a failure.
	WaitAfterFinished  timeutils.ParseableDuration `toml:"wait_after_finished,omitempty"` // A time period to wait after successful completion of the load testing before completely shutting the master down.
}

// MasterAuthConfig encapsulates authentication configuration for the load
// testing master node.
type MasterAuthConfig struct {
	Enabled      bool   `toml:"enabled"`       // Is basic HTTP authentication enabled?
	Username     string `toml:"username"`      // The username for accessing the master, if enabled.
	PasswordHash string `toml:"password_hash"` // The bcrypt hash of the password for accessing the master, if enabled.
}

// SlaveConfig provides configuration specific to the load testing slaves.
type SlaveConfig struct {
	Bind               string                      `toml:"bind"`   // The address to which to bind slave nodes (host:port).
	Master             ParseableURL                `toml:"master"` // The master's external address (URL).
	ExpectMasterWithin timeutils.ParseableDuration `toml:"expect_master_within"`
	ExpectStartWithin  timeutils.ParseableDuration `toml:"expect_start_within"`
	WaitAfterFinished  timeutils.ParseableDuration `toml:"wait_after_finished,omitempty"` // A time period to wait after successful completion of the load testing before completely shutting the slave down.
}

// TestNetworkConfig encapsulates information about the network under test.
type TestNetworkConfig struct {
	Autodetect TestNetworkAutodetectConfig `toml:"autodetect"`
	Targets    []TestNetworkTargetConfig   `toml:"targets"`              // Configuration for each of the Tendermint nodes in the network.
	OutageSim  TestNetworkOutageSimConfig  `toml:"outage_sim,omitempty"` // Configuration for the outage simulator.
}

// TestNetworkAutodetectConfig encapsulates information relating to the
// autodetection of the Tendermint test network nodes under test.
type TestNetworkAutodetectConfig struct {
	Enabled             bool                        `toml:"enabled"`        // Is target network autodetection enabled?
	SeedNode            ParseableURL                `toml:"seed_node"`      // The seed node from which to find other peers/targets.
	ExpectTargets       int                         `toml:"expect_targets"` // The number of targets to expect prior to starting load testing.
	ExpectTargetsWithin timeutils.ParseableDuration `toml:"expect_targets_within"`
	TargetSeedNode      bool                        `toml:"target_seed_node"` // Whether or not to include the seed node itself in load testing.
}

// TestNetworkOutageSimConfig encapsulates the outage simulator configuration
// for our test network.
type TestNetworkOutageSimConfig struct {
	Enabled    bool   `toml:"enabled"`            // Is the outage simulator enabled?
	Plan       string `toml:"plan"`               // The simulation plan.
	TargetPort int    `toml:"target_port"`        // The target port for all of the nodes' outage simulator instances.
	Username   string `toml:"username,omitempty"` // The username for accessing the outage simulator endpoints. Overridden by environment variable.
	Password   string `toml:"password,omitempty"` // The password for accessing the outage simulator endpoints. Overridden by environment variable.
}

// TestNetworkTargetConfig encapsulates the configuration for each node in the
// Tendermint test network.
type TestNetworkTargetConfig struct {
	ID  string `toml:"id"`  // A short, descriptive identifier for this node.
	URL string `toml:"url"` // The RPC URL for this target node.
}

// ParseConfig will parse the configuration from the given string.
func ParseConfig(data string) (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(data, &cfg); err != nil {
		return nil, NewError(ErrFailedToDecodeConfig, err)
	}
	// validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfig will attempt to load configuration from the given file.
func LoadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, NewError(ErrFailedToReadConfigFile, err)
	}
	return ParseConfig(string(data))
}

//
// Config
//

// Validate does a deep check on the configuration to make sure it makes sense.
func (c *Config) Validate() error {
	if err := c.Master.Validate(); err != nil {
		return err
	}
	if err := c.Slave.Validate(); err != nil {
		return err
	}
	if err := c.TestNetwork.Validate(); err != nil {
		return err
	}
	if err := c.Clients.Validate(); err != nil {
		return err
	}
	return nil
}

//
// MasterConfig
//

func (m *MasterConfig) Validate() error {
	if len(m.Bind) == 0 {
		return NewError(ErrInvalidConfig, nil, "master bind address must be specified")
	}
	if m.ExpectSlaves < 1 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("master must expect at least one slave, but got %d", m.ExpectSlaves))
	}
	return nil
}

//
// SlaveConfig
//

func (s *SlaveConfig) Validate() error {
	if len(s.Bind) == 0 {
		return NewError(ErrInvalidConfig, nil, "slave needs non-empty bind address")
	}
	if len(s.Master.String()) == 0 {
		return NewError(ErrInvalidConfig, nil, "slave address for master must be explicitly specified")
	}
	return nil
}

//
// TestNetworkConfig
//

func (c *TestNetworkConfig) Validate() error {
	// if we're autodetecting the network, no need to validate any explicit test
	// network config
	if c.Autodetect.Enabled {
		return c.Autodetect.Validate()
	}

	if len(c.Targets) == 0 {
		return NewError(ErrInvalidConfig, nil, "test network must have at least one target (found 0)")
	}
	for i, target := range c.Targets {
		if err := target.Validate(i); err != nil {
			return err
		}
	}

	return c.OutageSim.Validate()
}

//
// TestNetworkAutodetectConfig
//

func (c *TestNetworkAutodetectConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.SeedNode.String()) == 0 {
		return NewError(ErrInvalidConfig, nil, "test network autodetection requires a seed node address, but none provided")
	}
	if c.ExpectTargets <= 0 {
		return NewError(ErrInvalidConfig, nil, "test network autodetection requires at least 1 expected target to start testing")
	}
	return nil
}

// GetTargetRPCURLs will return a simple, flattened list of URLs for all of the
// target nodes' RPC addresses.
func (c *TestNetworkConfig) GetTargetRPCURLs() []string {
	urls := make([]string, 0)
	for _, target := range c.Targets {
		urls = append(urls, target.URL)
	}
	return urls
}

//
// TestNetworkTargetConfig
//

func (c *TestNetworkTargetConfig) Validate(i int) error {
	if len(c.ID) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("test network target %d is missing an ID", i))
	}
	if len(c.URL) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("test network target %d is missing its RPC URL", i))
	}
	return nil
}

//
// TestNetworkOutageSimConfig
//

func (c *TestNetworkOutageSimConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	username, password := os.Getenv(EnvOutageSimUser), os.Getenv(EnvOutageSimPassword)
	if len(username) > 0 {
		c.Username = username
	}
	if len(password) > 0 {
		c.Password = password
	}
	if len(c.Username) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("expected username for test network outage simulation config, but got none"))
	}
	if len(c.Password) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("expected password for test network outage simulation config, but got none"))
	}
	if len(c.Plan) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("outage simulator configuration plan is empty"))
	}
	return nil
}

//-----------------------------------------------------------------------------

// ParseableURL is a URL that we can parse from the configuration.
type ParseableURL url.URL

var _ encoding.TextUnmarshaler = (*ParseableURL)(nil)
var _ encoding.TextMarshaler = (*ParseableURL)(nil)

// UnmarshalText allows ParseableURL to implement encoding.TextUnmarshaler.
func (p *ParseableURL) UnmarshalText(text []byte) error {
	u, err := url.Parse(string(text))
	if err == nil {
		*p = ParseableURL(*u)
	}
	return err
}

// MarshalText converts the URL into a string.
func (p *ParseableURL) MarshalText() (text []byte, err error) {
	text = []byte(p.String())
	return
}

func (p *ParseableURL) String() string {
	u := url.URL(*p)
	return u.String()
}
