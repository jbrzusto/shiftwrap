package shiftwrap

import (
	"time"

	"github.com/sixdouglas/suncalc"
	"gopkg.in/yaml.v3"
)

// Config holds options for Shiftwrap
type Config struct {
	// Shell is the path to the shell used to run all Setup
	// and Takedown commands for shift changes and the idle
	// handler.
	Shell string //`yaml:"shell" json:"shell"`

	// DefaultMinRuntime is the minimum duration of a Shift in
	// order for a Service to be started.  It can be customized
	// for each Service.  This is meant to avoid thrashing or
	// other undesirable behaviour, especially when a Service
	// requires significant time for Setup or Takedown.
	// DefaultMinRuntime is always treated as a real duration,
	// even if ShiftWrap is using a dilated clock.
	DefaultMinRuntime TidyDuration `yaml:"default_min_runtime" json:"defaultMinRuntime"`

	// IdleHandlerCommand is a shell command to run when starting an idle period
	// These variables are set in its environment:
	//
	// SHIFTWRAP_IDLE_DURATION: the real duration of the idle
	// time, formatted as integer seconds
	//
	// SHIFTWRAP_IDLE_DURATION_DILATED: the dilated duration of
	// the idle time, formatted as integer seconds; only
	// differs from SHIFTWRAP_IDLE_DURATION_DILATED if ShiftWrap
	// is using an AClock with warped time.
	//
	// SHIFTWRAP_NEXT_EVENT_TIME: the real time of the next
	// ShiftEvent, formatted as integer seconds since the
	// Unix Epoch
	//
	// SHIFTWRAP_NEXT_EVENT_TIME_DILATED: the dilated time of the
	// next ShiftEvent, formatted as integer seconds since
	// the Unix Epoch; only differs from
	// SHIFTWRAP_NEXT_EVENT_TIME if ShiftWrap is using an AClock
	// with warped time.
	IdleHandlerCommand string `yaml:"idle_handler_command" json:"idleHandlerCommand"`

	// IdleHandlerInitialDelay is the minimum time shiftwrapd must
	// run before it is allowed to call the idle handler.  This is
	// a simple way to cause shiftwrapd to wait for all enabled
	// shiftwrap-managed services to be started (i.e. added to
	// shiftwrapd) at boot time.  It can take some time for
	// devices to be enumerated, and some of these might cause an
	// instantiated shiftwwrap-managed service to be started from
	// udevd.  This parameter also provides a "safety window" of
	// time after booting during which the system will not be put
	// to sleep by shiftwrapd, so that e.g. it can be reached by
	// ssh for reconfiguration (in case shift definitions would
	// otherwise cause it to sleep immediately).
	// IdleHandlerInitialDelay is always treated as a real duration,
	// even if ShiftWrap is using a dilated clock.
	IdleHandlerInitialDelay TidyDuration `yaml:"idle_handler_initial_delay" json:"idleHandlerInitialDelay"`

	// IdleHandlerMinRuntime is the minimum idle time required
	// before the idle handler is called.  e.g. if the idle
	// handler uses rtcwake to sleep the system, and it is found
	// to require 500 ms in addition to the sleep time in order to
	// restore the system state, then we don't want to call it for
	// an idle period of less than that.  For idle periods less
	// than this value, shiftwrapd simply waits for the next
	// ShiftEvent.
	IdleHandlerMinRuntime TidyDuration `yaml:"idle_handler_min_runtime" json:"idleHandlerMinRuntime"`

	// ServerAddress is the IP:PORT address for the http server by which
	// shiftwrapd is controlled
	ServerAddress string `yaml:"server_address" json:"serverAddress"`

	// Observer is the lat/long/height of the location at which ShiftWrap is running; used for
	// calculating times of solar events
	Observer suncalc.Observer `yaml:"observer" json:"observer"`

	// LocationName is the name of the timezone for the Observer
	LocationName string `yaml:"location" json:"location"`
}

var DefaultConfig = Config{
	IdleHandlerInitialDelay: TidyDuration(2 * time.Second),
	IdleHandlerMinRuntime:   TidyDuration(2 * time.Second),
	IdleHandlerCommand:      "echo would do sudo rtc_wake -m mem -s $SHIFTWRAP_IDLE_DURATION",
	Shell:                   "/bin/bash",
	DefaultMinRuntime:       TidyDuration(1 * time.Minute),
	ServerAddress:           ":9009",
}

func (c *Config) Parse(buf []byte) (err error) {
	if err = yaml.Unmarshal(buf, c); err != nil {
		return
	}
	c.Observer.Location, err = time.LoadLocation(c.LocationName)
	return
}
