package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// Config for the run.
type Config struct {
	Proto        string                  `json:"proto"`
	Call         string                  `json:"call"`
	Cert         string                  `json:"cert"`
	N            int                     `json:"n"`
	C            int                     `json:"c"`
	QPS          int                     `json:"qps"`
	Z            time.Duration           `json:"z"`
	Timeout      int                     `json:"timeout"`
	Data         *map[string]interface{} `json:"data,omitempty"`
	DataPath     string                  `json:"dataPath"`
	Metadata     *map[string]string      `json:"metadata,omitempty"`
	MetadataPath string                  `json:"metadataPath"`
	Format       string                  `json:"format"`
	Output       string                  `json:"output"`
	Host         string                  `json:"host"`
	CPUs         int                     `json:"cpus"`
	ImportPaths  []string                `json:"importPaths,omitempty"`
}

// Default implementation.
func (c *Config) Default() {
	if c.N == 0 {
		c.N = 200
	}

	if c.C == 0 {
		c.C = 50
	}

	if c.CPUs == 0 {
		c.CPUs = runtime.GOMAXPROCS(-1)
	}
}

// Validate implementation.
func (c *Config) Validate() error {
	if err := requiredString(c.Proto); err != nil {
		return errors.Wrap(err, "proto")
	}

	if filepath.Ext(c.Proto) != ".proto" {
		return errors.Errorf(fmt.Sprintf("proto: must have .proto extension"))
	}

	if err := requiredString(c.Call); err != nil {
		return errors.Wrap(err, "call")
	}

	if err := minValue(c.N, 0); err != nil {
		return errors.Wrap(err, "n")
	}

	if err := minValue(c.C, 0); err != nil {
		return errors.Wrap(err, "c")
	}

	if err := minValue(c.QPS, 0); err != nil {
		return errors.Wrap(err, "q")
	}

	if err := minValue(c.Timeout, 0); err != nil {
		return errors.Wrap(err, "t")
	}

	if err := minValue(c.CPUs, 0); err != nil {
		return errors.Wrap(err, "cpus")
	}

	if strings.TrimSpace(c.DataPath) == "" {
		// if err := RequiredString(c.Data); err != nil {
		// 	return errors.Wrap(err, "data")
		// }
		if c.Data == nil {
			return errors.New("data: is required")
		}
	}

	return nil
}

// UnmarshalJSON is our custom implementation to handle the Duration field Z
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		Z string `json:"z"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.Z, _ = time.ParseDuration(aux.Z)
	return nil
}

// MarshalJSON is our custom implementation to handle the Duration field Z
func (c Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		*Alias
		Z string `json:"z"`
	}{
		Alias: (*Alias)(&c),
		Z:     c.Z.String(),
	})
}

// InitData returns the payload data
func (c *Config) InitData() error {
	if c.Data != nil {
		return nil
	} else if strings.TrimSpace(c.DataPath) != "" {
		d, err := ioutil.ReadFile(c.DataPath)
		if err != nil {
			return err
		}

		return json.Unmarshal(d, &c.Data)
	}

	return errors.New("No data specified")
}

// SetData sets data based on input JSON string
func (c *Config) SetData(in string) error {
	if strings.TrimSpace(in) != "" {
		return json.Unmarshal([]byte(in), &c.Data)
	}
	return nil
}

// SetMetadata sets the metadata based on input JSON string
func (c *Config) SetMetadata(in string) error {
	if strings.TrimSpace(in) != "" {
		return json.Unmarshal([]byte(in), &c.Metadata)
	}
	return nil
}

// InitMetadata returns the payload data
func (c *Config) InitMetadata() error {
	if c.Metadata != nil && len(*c.Metadata) > 0 {
		return nil
	} else if strings.TrimSpace(c.MetadataPath) != "" {
		d, err := ioutil.ReadFile(c.MetadataPath)
		if err != nil {
			return err
		}

		return json.Unmarshal(d, &c.Metadata)
	}

	return nil
}

// RequiredString checks if the required string is empty
func requiredString(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("is required")
	}

	return nil
}

func minValue(v int, min int) error {
	if v < min {
		return errors.Errorf(fmt.Sprintf("must be at least %d", min))
	}

	return nil
}

// ReadConfig reads config from path
func ReadConfig(path string) (*Config, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseConfig(b)
}

func parseConfigString(s string) (*Config, error) {
	return parseConfig([]byte(s))
}

func parseConfig(b []byte) (*Config, error) {
	c := &Config{}

	if err := json.Unmarshal(b, c); err != nil {
		return nil, errors.Wrap(err, "parsing json")
	}

	c.Default()

	if err := c.Validate(); err != nil {
		return nil, errors.Wrap(err, "validating")
	}

	return c, nil
}