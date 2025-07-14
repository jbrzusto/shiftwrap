package shiftwrap

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jbrzusto/timewarper"
	"github.com/sixdouglas/suncalc"
	"gopkg.in/yaml.v3"
)

// SolarEvents is a map from solar event name to DayTime; it is used
// for cacheing calculated daily solar event times.
type SolarEvents map[suncalc.DayTimeName]suncalc.DayTime

// ShiftWrap manages running of some systemd services in one or more daily shifts.
type ShiftWrap struct {
	// Config holds settings for this ShiftWrap
	Conf Config
	// NumManagedServices counts the number of services with IsManaged==true
	NumManagedServices int
	// services is a map from Service.Name to *Service, of configured services.
	// A configured Service is managed by Shiftwrap iff Service.IsManaged == true
	services map[string]*Service
	// shiftChangeListeners is a slice of chan ShiftChange which receives
	// all the ShiftChanges as they occur.  These will have the .trueAt field set.
	shiftChangeListeners []chan ShiftChange
	// Clock is an alternative clock.  It allows ShiftWrap to use either the standard
	// operating system clock, or a warped clock that e.g. runs faster and is shifted into
	// the future.  This allows testing ShiftWrap scenarios much faster than realtime.
	Clock timewarper.AClock
	// Hurry flags that ShiftWrap's Scheduler should run as
	// quickly as possible, i.e. jumping from one ShiftChange to the next, without waiting
	// for the prescribed time to elapse.  This is achieved by calling Clock.JumpToFutureTime(),
	// and is intended for testing.
	Hurry bool
	// schedChan is a channel for communicating SchedMsg events to the scheduler.
	schedChan chan SchedMsg
	// scQueue is a queue of ShiftChange, ordered by their At field
	scQueue *ShiftChangeQueue
	// scTimer is a timer counting down to the next Shift Change
	scTimer timewarper.ATimer
	// numRunningServices counts the number of managed services which are running
	numRunningServices int
	// startTime is the real time when this ShiftWrap was created
	startTime time.Time
	// systemctlUserOpt is an option to all calls to `systemctl` which is either '--system' or '--user',
	// depending on whether the process which created this ShiftWrap is running as root.
	systemctlUserOpt string
	// nextShiftChangeID is the id for the next-created ShiftChange
	nextShiftChangeID uint64
	// nextShiftChangeIDMtx protects nextShiftChangeID
	nextShiftChangeIDMtx sync.Mutex
	// prevHeadID is the id of the previous head of the ShiftChange queue
	prevHeadID uint64
}

// TidyDuration is a time.Duration which is printed in "AhBmC.Ds" format
type TidyDuration time.Duration

// Service describes a systemd service to run in shifts
type Service struct {
	// Name is the name of this Service
	Name string
	// IsManaged is true when shiftwrap is managing the service.  A Service is configured
	// with an X.yml file in the /etc/shiftwrap directory, but whether it is managed or not
	// is determined by use of systemctl; e.g. systemctl enable shitwrap@myservice
	IsManaged bool `yaml:"-"`
	// IsSystemd is true if Name refers to a systemd service (which must have a .service
	// file in one of the standard systemd directories.
	IsSystemd bool
	// MinRuntime is the minimum time Service should be run; shorter shifts are dropped
	// without running Setup or Takedown
	//	MinRuntime time.Duration
	MinRuntime TidyDuration
	// Shifts is a map from Shift.Name to &Shift
	Shifts NamedShifts
	// shiftChanges is the most recently-calculated slice of ShiftChange for
	// this service.  It is sorted by increasing .At field, and overlaps have
	// been merged.
	shiftChanges ShiftChanges
	// shiftChangeIndex tracks which shift we are currently in; -1 means not yet known
	shiftChangeIndex int
	// running is cached running status.  This item is definitive if Name == "", because
	// then there is no systemd service to start/stop (only Setup and Takedown
	// scripts are run).
	// Initially, service running status is checked by calling Service.CheckIfRunning()
	running bool
	// haveCheckedRunning is false initially, true after using systemctl to check whether
	// the service is running.
	haveCheckedRunning bool
	// shiftwrap is the ShiftWrap which owns this service, if any
	shiftwrap *ShiftWrap
}

// ServiceEvent represents a change in a Service
type ServiceEvent int

const (
	// SchedulerReady is sent when the Scheduler for a Service has been started
	SchedulerReady = iota
	// SchedulerDone is sent when the Scheduler for a Service has been stopped
	SchedulerDone
	// ServiceRemoved is sent when a Service has been removed from the ShiftWrap
	// e.g. from a call to ShiftWrap.ShutDown()
	ServiceRemoved
	// ShiftsRedefined is sent when a Service's Shift definitions have changed
	ShiftsRedefined
)

// Shift defines a portion of the day during which a Service should run,
// based on fixed times of day and/or solar events with an optional offset.
// On a given date, a Shift can cross the midnight boundary, and it can be empty.
type Shift struct {
	// service is the service to which this shift definition belongs
	service *Service
	// Label uniquely identifies this ShiftDef within Service
	Label string
	// Start specifes the time, possibly relative to a solar event, at which the shift begins
	Start *TimeSpec
	// Stop specifes the time, possibly relative to a solar event, at which the shift ends
	Stop *TimeSpec
	// MustInclude specifes a time which must be included in the shift in order to use it.
	MustInclude *TimeSpec
	// MustExclude specifes a time which must *not* be included in the shift in order to use it.
	MustExclude *TimeSpec
	// Setup is code to run in a shell before the service is started for this shift
	Setup string
	// Takedown is code to run in a shell after the service is stopped for this shift
	Takedown string
}

// NamedShifts is a map from Shift.Label to Shift
type NamedShifts map[string]*Shift

// TimeSpec allows a time of day to be given as either an absolute time of day, or a time
// relative to a solar event.
type TimeSpec struct {
	// Origin is the name of a solar event; "" means clock midnight.
	Origin suncalc.DayTimeName
	// Offset from Origin.  When Origin=="", this is not used and TimeOfDay is
	Offset time.Duration
	// TimeOfDay is the time of day (on the date 1 Jan 0) of the event;
	// only used if Origin==""
	TimeOfDay time.Time
}

// ShiftChangeType is the type of ShiftChangeType
// Two of these are real (Start, Stop), and the other two
// are just convenient ways to represent other times associated
// with a Shift definition.
type ShiftChangeType uint8

const (
	Start ShiftChangeType = iota
	Stop
	MustInclude
	MustExclude
)

// ShiftChange is the start or end of a specific shift (if Type = Start or Stop)
// or a time which must be included or excluded (if Type = MustInclude or MustExclude).
// starts or ends on a particular day.
type ShiftChange struct {
	// id is a unique id for this shiftchange, through an entire session
	// of ShiftWrap.  If id == 0, this is a zero ShiftChange
	id uint64
	// shift specifies the definition of the Shift
	shift *Shift
	// At specifies the time for the shift change
	At time.Time
	// Type indicates whether this is the start, end or other timespec
	Type ShiftChangeType
	// trueAt, if not zero, is the time the shift change actually occurred
	trueAt time.Time
}

// ShiftChanges is a slice of ShiftChange
type ShiftChanges []ShiftChange

// ShiftChangeSinglet is a Start, Stop and optionally MustInclude and MustExclude Shift Changes,
// calculated for one Shift on one day.  MustInclude and MustExclude can satisfy IsZero()
type ShiftChangeSinglet struct {
	Start       ShiftChange
	Stop        ShiftChange
	MustInclude ShiftChange
	MustExclude ShiftChange
}

// SchedMsgType is a type of message sent to or by the scheduler
type SchedMsgType int

const (
	SMManageService SchedMsgType = iota + 1
	SMUnmanageService
	SMQuit
	SMConfirm
)

// SchedMsg is a message sent to the Scheduler;
// it has an optional parameter pointing to a Service,
// where appropriate.
type SchedMsg struct {
	Type SchedMsgType
	*Service
}

// Parse reads a Shift from a yml definition.
func (sh *Shift) Parse(buf []byte) (err error) {
	err = yaml.Unmarshal(buf, sh)
	return
}

// ShiftChangesOn returns a ShiftChangeSinglet for Shift sh on date d.
// The time component of d should be local noon.
func (sw *ShiftWrap) ShiftChangesOn(sh *Shift, d time.Time) (rv ShiftChangeSinglet) {
	rv.Start = sw.NewShiftChange(sh, sw.Time(d, sh.Start), Start)
	rv.Stop = sw.NewShiftChange(sh, sw.Time(d, sh.Stop), Stop)
	if sh.MustInclude != nil {
		rv.MustInclude = sw.NewShiftChange(sh, sw.Time(d, sh.MustInclude), MustInclude)
	}
	if sh.MustExclude != nil {
		rv.MustExclude = sw.NewShiftChange(sh, sw.Time(d, sh.MustExclude), MustExclude)
	}
	return
}

// AddSinglet adds some or all of the non-zero ShiftChanges from a ShiftChangeSinglet
// to scs. Only those which can be part of a shift overlapping the day given by daystart,
// dayend are added.  Returns true if at least one Start or Stop ShiftChange was added.
func (scs *ShiftChanges) AddSinglet(sg ShiftChangeSinglet, daystart, dayend time.Time) (rv bool) {
	//	log.Printf("adding singlet %v to %s\n", sg, daystart.Format(time.DateOnly))
	if sg.Start.At.Before(sg.Stop.At) {
		// "same-day" shift
		if !(daystart.After(sg.Stop.At) || dayend.Before(sg.Start.At)) {
			// shift overlaps day
			*scs = append(*scs, sg.Start, sg.Stop)
			rv = true
		}
	} else {
		// "overnight shift"
		// add Start if before end of day but after 1 day before
		if !sg.Start.At.After(dayend) && sg.Start.At.After(daystart.AddDate(0, 0, -1)) {
			*scs = append(*scs, sg.Start)
			rv = true
		}
		// add Stop if after start of day but before 1 day after
		if !sg.Stop.At.Before(daystart) && sg.Stop.At.Before(dayend.AddDate(0, 0, 1)) {
			*scs = append(*scs, sg.Stop)
			rv = true
		}
	}
	// add any non-zero includes/excludes
	if !sg.MustInclude.IsZero() {
		*scs = append(*scs, sg.MustInclude)
	}
	if !sg.MustExclude.IsZero() {
		*scs = append(*scs, sg.MustExclude)
	}
	return
}

// ShiftChangesAffecting returns a slice of ShiftChanges (include
// Exclude and Include times) for Shift sh which could affect the day
// containing d.  Enumeration starts with d and continues with adjacent days
// in each direction until a day is found which adds no ShiftChanges.
// The set of all ShiftChanges calculated for the days from the earliest to the latest
// is returned.
func (sw *ShiftWrap) ShiftChangesAffecting(sh *Shift, d time.Time) (rv ShiftChanges) {
	daystart, dayend := DayStartEnd(d)
	day := sw.ShiftChangesOn(sh, d)
	rv.AddSinglet(day, daystart, dayend)
	for dd := 1; dd > -2; dd -= 2 {
		done := false
		for i := 1; i < 365 && !done; i++ {
			day = sw.ShiftChangesOn(sh, d.AddDate(0, 0, i*dd))
			done = !rv.AddSinglet(day, daystart, dayend)
		}
		if !done {
			dir := "after"
			if dd < 0 {
				dir = "before"
			}
			log.Printf("weird - at least 365 days %s %s have shifts affcting it, for service %s, shift %s", dir, d.Format(time.DateTime), sh.Service().Name, sh.Label)
		}
	}
	rv.Sort()
	return
}

// ShiftRunningPeriods returns a slice of ShiftChanges, defined by Shift
// sh, which give running periods that overlap day d.  That is,
// len(rv) is even, rv is sorted in order of ascending .At field, and
// elements of rv alternate between Type==Start and Type==Stop,
// beginning with the former.  These periods will satisfy MustInclude
// and/or MustExclude conditions, if sh specifies these.  Running periods
// go from a Start ShiftChange to the next Stop ShiftChange.
// The time component of d should be local noon.
func (sw *ShiftWrap) ShiftRunningPeriods(sh *Shift, d time.Time) (rv ShiftChanges) {
	scs := sw.ShiftChangesAffecting(sh, d)
	//	fmt.Printf("SCA %s:\n%s\n", d.Format(time.DateOnly), scs.String())
	daystart, dayend := DayStartEnd(d)
	haveInc := !sh.MustInclude.IsZero()
	haveExc := !sh.MustExclude.IsZero()
	// loop, finding a Start
	foundStart, foundInc, foundExc := false, false, false
	dest := 0
	for _, sc := range scs {
		switch sc.Type {
		case Start:
			if !foundStart {
				scs[dest] = sc
				dest++
				foundStart = true
			} else {
				// replace start.
				scs[dest-1] = sc
				foundInc = false
				foundExc = false
			}
		case MustInclude:
			if foundStart {
				foundInc = true
			}
		case MustExclude:
			if foundStart {
				foundExc = true
			}
		case Stop:
			if foundStart {
				if ((haveInc && foundInc) || !haveInc) &&
					((haveExc && !foundExc) || !haveExc) &&
					scs[dest-1].At.Before(dayend) &&
					sc.At.After(daystart) {
					// running period valid, so add this Stop
					scs[dest] = sc
					dest++
				} else {
					// running period not valid, so drop the start
					dest--
				}
				foundStart, foundInc, foundExc = false, false, false
			} else {
				// ignore stray Stop
			}
		}
	}
	if foundStart {
		// remove dangling Start
		dest--
	}
	rv = scs[0:dest]
	return
}

// ServiceRunningPeriods returns a slice of ShiftChanges from all Shifts
// for service s; these define running periods which overlap day d.  The
// running periods from different shifts are merged when they overlap, and
// then any shorter than MinRuntime are removed.
// The time component of d should be local noon.
func (sw *ShiftWrap) ServiceRunningPeriods(s *Service, d time.Time, raw bool) (rv ShiftChanges) {
	for _, sh := range s.Shifts {
		rv = append(rv, sw.ShiftRunningPeriods(sh, d)...)
	}
	rv.Sort()
	if !raw {
		rv = sw.RemoveShorts(sw.MergeOverlaps(rv))
	}
	return
}

// Parse decodes a yaml representation of a Service,
// filling in the Shift.Label fields
func (s *Service) Parse(buf []byte) (err error) {
	if err = yaml.Unmarshal(buf, s); err != nil {
		return
	}
	for n, sh := range s.Shifts {
		sh.Label = n
	}
	return
}

// Service is a convenience function that returns a pointer to the
// Service to which a Shift belongs.
func (s *Shift) Service() *Service {
	return s.service
}

func (ts *TimeSpec) IsZero() bool {
	return ts == nil || ts.Origin == "" && ts.TimeOfDay.IsZero()
}

func (ts *TimeSpec) UnmarshalYAML(value *yaml.Node) (err error) {
	*ts, err = ParseTimeSpec(value.Value)
	return
}

func (ts *TimeSpec) UnmarshalJSON(value []byte) (err error) {
	*ts, err = ParseTimeSpec(string(value[1 : len(value)-1]))
	return
}

func (ts TimeSpec) MarshalYAML() (value any, err error) {
	value = ts.String()
	return
}

func (ts TimeSpec) MarshalJSON() (value []byte, err error) {
	value = []byte(`"` + ts.String() + `"`)
	return
}

func (sc ShiftChange) MarshalJSON() (value []byte, err error) {
	value = []byte(fmt.Sprintf(`{"Service":"%s", "Label":"%s","At":"%s","IsStart":%t}`, sc.Service().Name, sc.Shift().Label, sc.At, sc.Type == Start))
	return
}

// IsZero checks whether the ShiftChange is zero
func (sc ShiftChange) IsZero() bool {
	return sc.id == 0
}

// String formats ShiftChange as a string
func (sc *ShiftChange) String() (rv string) {
	var verb, when string
	if sc.id == 0 {
		rv = "Zero  shift"
		return
	}
	switch sc.Type {
	case Start:
		verb = "Start"
		when = sc.Shift().Start.String()
	case Stop:
		verb = "Stop "
		when = sc.Shift().Stop.String()
	case MustInclude:
		verb = "Inc  "
		when = sc.Shift().MustInclude.String()
	case MustExclude:
		verb = "Exc "
		when = sc.Shift().MustExclude.String()
	}
	rv = verb + " shift " + sc.Shift().Label + " at " + when
	return
}

// String formats ShiftChanges as a string
func (scs ShiftChanges) String() (rv string) {
	for _, sc := range scs {
		rv = rv + sc.String() + ": " + sc.At.Format(time.RFC3339Nano) + "\n"
	}
	return
}

// Shift is a convenience function that returns a pointer to the
// Shift to which a ShiftChange belongs.
func (s *ShiftChange) Shift() *Shift {
	return s.shift
}

// Service is a convenience function that returns a pointer to the
// Service to which a ShiftChange belongs.
func (s *ShiftChange) Service() *Service {
	return s.Shift().Service()
}

// Equals returns true if sc and sc2 represent the same effective shift change,
// i.e. ignoring allocation order (.id) or time of ocurrence (.trueAt)
func (sc ShiftChange) Equals(sc2 ShiftChange) bool {
	return sc.shift == sc2.shift && sc.At == sc2.At && sc.Type == sc2.Type
}

var DefaultShiftWrap *ShiftWrap

// NewShiftWrap returns a *ShiftWrap that uses the system clock
func NewShiftWrap() (rv *ShiftWrap) {
	return NewShiftWrapWithAClock(timewarper.GetStandardClock())
}

// NewShiftWrapWithAClock returns a *ShiftWrap that uses the specified AClock
func NewShiftWrapWithAClock(c timewarper.AClock) (rv *ShiftWrap) {
	rv = &ShiftWrap{
		Conf:               DefaultConfig,
		Clock:              c,
		services:           make(map[string]*Service),
		scQueue:            NewShiftChangeQueue(),
		scTimer:            c.NewStoppedATimer(),
		numRunningServices: 0,
		NumManagedServices: 0,
		schedChan:          make(chan SchedMsg),
		startTime:          time.Now(),
		nextShiftChangeID:  1, // avoid generating zero-valued ShiftChange
	}
	if os.Geteuid() == 0 {
		rv.systemctlUserOpt = "--system"
	} else {
		rv.systemctlUserOpt = "--user"
	}
	go rv.Scheduler()
	return
}

// Quit causes the Scheduler for the ShiftWrap to stop, freeing
// all resources for gc and ending any associated threads.
func (sw *ShiftWrap) Quit() {
	sw.schedChan <- SchedMsg{Type: SMQuit}
	<-sw.schedChan
}

// NewShiftChange creates a new ShiftChange for the given
// shift, time, and start flag
func (sw *ShiftWrap) NewShiftChange(sh *Shift, at time.Time, sct ShiftChangeType) (rv ShiftChange) {
	sw.nextShiftChangeIDMtx.Lock()
	defer sw.nextShiftChangeIDMtx.Unlock()
	rv = ShiftChange{
		shift: sh,
		At:    at,
		Type:  sct,
		id:    sw.nextShiftChangeID,
	}
	sw.nextShiftChangeID++
	return
}

// NewShiftChangeListener adds a listener for ShiftChanges;
// this is implemented as a new channel on which ShiftChange
// are sent as they are triggered.
func (sw *ShiftWrap) NewShiftChangeListener() chan ShiftChange {
	rv := make(chan ShiftChange)
	sw.shiftChangeListeners = append(sw.shiftChangeListeners, rv)
	return rv
}

// MinRuntime returns the minimum runtime for a Service.  If the Service
// has the zero value for this field, the DefaultMinRunTime from the ShiftWrap's
// Config field is used
func (sw *ShiftWrap) MinRuntime(s *Service) time.Duration {
	if s.MinRuntime != 0 {
		return time.Duration(s.MinRuntime)
	}
	return sw.Conf.DefaultMinRuntime
}

var activeStatus = map[string]bool{
	"active":       true,
	"start-pre":    true,
	"start":        true,
	"start-post":   true,
	"running":      true,
	"reload":       true,
	"auto-restart": true,
}

// CheckIfRunning returns true if Name != "" and Service is running or in
// the process of starting up, according to systemd.  If Name == "", returns
// the isrunning flag, which indicates that the Setup script has run successfully
// more recently than the Takedown script has.
func (s *Service) CheckIfRunning() bool {
	if s.Name == "" {
		// there's no real service; only Setup and Takedown scripts are run
		return s.running
	}
	out, err := exec.Command("systemctl", s.ShiftWrap().systemctlUserOpt, "show", "-p", "SubState", "--value", s.Name).Output()
	if err != nil {
		log.Fatalf("unable to check running status of %s: %s", s.Name, err.Error())
	}
	ss := strings.TrimSpace(string(out))
	s.haveCheckedRunning = true
	return activeStatus[ss]
}

// IsRunning returns true if the Service is marked as running; systemd is queried if necessary
func (s *Service) IsRunning() bool {
	if !s.IsSystemd || s.haveCheckedRunning {
		return s.running
	}
	s.running = s.CheckIfRunning()
	return s.running
}

// GetCurrentShiftChanges returns the current ShiftChanges for the service
func (s *Service) GetCurrentShiftChanges() ShiftChanges {
	return s.shiftChanges
}

// GetQueue returns the queue of ShiftChanges
func (sw *ShiftWrap) GetQueue() ShiftChanges {
	return sw.scQueue.ShiftChanges
}

// GetTimer returns the current ShiftChange timer
func (sw *ShiftWrap) GetTimer() time.Time {
	return sw.scTimer.Target()
}

// Start starts a Shift for a Service, after running any Setup script.
// If the Service.IsSystemd is false, mark it as running if the Setup script
// runs without error.
// If sh is nil, the Shift is considered "unknown" and Setup is not run.
// Returns false if either the Setup script or the "systemctl start ..." command
// fail, or if Service.IsSystemd is false and Service.running is true; returns true otherwise.
func (sw *ShiftWrap) Start(s *Service, sh *Shift, t time.Time) bool {
	var label string
	if sh != nil {
		label = sh.Label
	} else {
		label = "(unknown)"
	}
	if !s.IsSystemd && s.running {
		log.Printf("non-systemd Service %s is already running", s.Name)
		return false
	}
	if sh != nil && sh.Setup != "" {
		// log.Printf("running Setup for shift %s of service %s", label, s.Name)
		cmd := exec.Command(sw.Conf.Shell, "-c", sh.Setup)
		cmd.Env = append(cmd.Environ(),
			fmt.Sprintf("SHIFTWRAP_SERVICE=%s", s.Name),
			fmt.Sprintf("SHIFTWRAP_SHIFT=%s", label),
			fmt.Sprintf("SHIFTWRAP_TIME=%d", t.UnixNano()/1e9),
			"SHIFTWRAP_ACTION=setup",
		)
		if buf, err := cmd.Output(); err != nil {
			log.Printf("error when running Setup script for shift '%s' of service %s: %s", label, s.Name, err.Error())
			return false
		} else {
			log.Printf("%s: Setup for shift '%s': %s", s.Name, label, buf)
		}
	}
	if s.IsSystemd {
		// log.Printf("starting shift %s of service %s", label, s.Name)
		if err := exec.Command("systemctl", s.ShiftWrap().systemctlUserOpt, "start", s.Name).Run(); err != nil {
			log.Printf("error when starting shift '%s' of service %s: %s", label, s.Name, err.Error())
			return false
		}
	}
	// log.Printf("setting running status to true for service %s", s.Name)
	s.running = true
	sw.numRunningServices++
	return true
}

// Stop stops a shift for a service, including any Takedown script.
// It can also stop Service without a known shift (i.e. with sh == nil),
// in which case no Takedown code is run.
// If Service.IsSystemd is false, sets Service.running to false if the Takedown script
// runs without error.
// Returns false if either the Takedown script or the "systemctl stop ..." command
// fail, or if Service.IsSystemd is false and Service.running is also false; returns true otherwise.
func (sw *ShiftWrap) Stop(s *Service, sh *Shift, t time.Time) bool {
	var label string
	if sh != nil {
		label = sh.Label
	} else {
		label = "(unknown)"
	}
	if s.IsSystemd {
		// log.Printf("stopping shift %s of service %s", label, s.Name)
		if err := exec.Command("systemctl", s.ShiftWrap().systemctlUserOpt, "stop", s.Name).Run(); err != nil {
			log.Printf("error when stopping shift '%s' of service %s: %s", label, s.Name, err.Error())
			return false
		}
	} else if !s.running {
		return false
	}
	if sh != nil && sh.Takedown != "" {
		// log.Printf("running Takedown for shift %s of service %s", sh.Label, s.Name)
		cmd := exec.Command(sw.Conf.Shell, "-c", sh.Takedown)
		cmd.Env = append(cmd.Environ(),
			fmt.Sprintf("SHIFTWRAP_SERVICE=%s", s.Name),
			fmt.Sprintf("SHIFTWRAP_SHIFT=%s", sh.Label),
			fmt.Sprintf("SHIFTWRAP_TIME=%d", t.UnixNano()/1e9),
			"SHIFTWRAP_ACTION=takedown",
		)
		if buf, err := cmd.Output(); err != nil {
			log.Printf("error when running Takedown script for shift '%s' of service %s: %s", sh.Label, s.Name, err.Error())
			return false
		} else {
			log.Printf("%s: Takedown for shift '%s': %s", s.Name, sh.Label, buf)
		}
	}
	// log.Printf("setting running status to false for service %s", s.Name)
	s.running = false
	sw.numRunningServices--
	return true
}

// ServiceByName retrieves a service by its name.  If all of these are true:
// - the name includes a '@' (i.e. it is an instantiated service)
// - the service does not exit
// - a service template exists (with the name up to and including the '@'),
// then a new Service is created by copying the service template.
// Also, if create is true, then if the service does not already exist and is
// not an instantiated service, it is created.
// If no Service can be found or instantiated for name sn, and create is false,
// the function returns nil.
func (sw *ShiftWrap) ServiceByName(sn string, create bool) (rv *Service) {
	if rv = sw.services[sn]; rv != nil {
		return
	}
	tpn, _, isInstance := strings.Cut(sn, "@")
	if isInstance {
		tp := sw.services[tpn+"@"]
		if tp != nil {
			// found a template, so instantiate it
			rv = tp.Instantiate(sn)
			sw.AddService(rv)
		}
		return
	}
	if create {
		rv = &Service{
			Name: sn,
		}
		sw.AddService(rv)
	}
	return
}

// GetServiceNames returns a list of configured Services.
func (sw *ShiftWrap) GetServiceNames() (rv []string) {
	for n := range sw.services {
		rv = append(rv, n)
	}
	return
}

// GetDefaultShiftWrap returns the default ShiftWrap object,
// creating it if it does not exist.
func GetDefaultShiftWrap() *ShiftWrap {
	if DefaultShiftWrap == nil {
		DefaultShiftWrap = NewShiftWrap()
	}
	return DefaultShiftWrap
}

// ShiftWrap returns the ShiftWrap that owns the Service;
// returns
func (s *Service) ShiftWrap() *ShiftWrap {
	if s.shiftwrap == nil {
		return GetDefaultShiftWrap()
	}
	return s.shiftwrap
}

// Instantiate creates an instance of a templated Service.
// It is passed the full service name, e.g. `eat-serial@ttyS0`
func (s *Service) Instantiate(fullName string) (rv *Service) {
	rv = &Service{
		Name:         fullName,
		IsManaged:    false,
		IsSystemd:    s.IsSystemd,
		MinRuntime:   s.MinRuntime,
		Shifts:       NamedShifts{},
		shiftChanges: nil,
		running:      false,
		shiftwrap:    s.shiftwrap,
	}
	for n, sh := range s.Shifts {
		rv.Shifts[n] = &Shift{
			service:  rv,
			Label:    sh.Label,
			Start:    sh.Start,
			Stop:     sh.Stop,
			Setup:    sh.Setup,
			Takedown: sh.Takedown,
		}
	}
	return
}

// AddService adds service s to the pool of configured Services.
func (sw *ShiftWrap) AddService(s *Service) (err error) {
	if _, have := sw.services[s.Name]; have {
		err = fmt.Errorf("service %s already defined", s.Name)
		return
	}
	sw.services[s.Name] = s
	s.shiftwrap = sw
	if s.MinRuntime == 0 {
		s.MinRuntime = TidyDuration(sw.Conf.DefaultMinRuntime)
	}
	if s.Shifts == nil {
		s.Shifts = map[string]*Shift{}
	}
	return
}

// ManageService starts (if manage is true) or stops managing a
// configured service.  If the service is already managed (or not
// managed, as appropriate), ManageService does nothing.
func (sw *ShiftWrap) ManageService(s *Service, manage bool) (err error) {
	if s == nil {
		err = fmt.Errorf("service is nil at ManageServce")
		return
	}
	msg := SchedMsg{Service: s}
	if manage {
		if s.IsManaged {
			return
		}
		msg.Type = SMManageService
	} else {
		if !s.IsManaged {
			return
		}
		msg.Type = SMUnmanageService
	}
	sw.schedChan <- msg
	<-sw.schedChan
	return
}

// DropService removes the service named sn from the pool of
// configured services.  If the service was being managed by Shiftwrap,
// it will no longer be, but regardless, DropService does not affect
// the running state of service sn (e.g. does not do `systemctl stop` on sn)
func (sw *ShiftWrap) DropService(sn string) (err error) {
	var (
		s    *Service
		have bool
	)
	if s, have = sw.services[sn]; !have {
		err = fmt.Errorf("service %s not defined", sn)
		return
	}
	if s.IsManaged {
		sw.ManageService(s, false)
	}
	delete(sw.services, sn)
	return
}

// CalculateShiftChanges recalculates ShiftChanges for a Service,
// and returns the time for which the calculation occurred.
func (sw *ShiftWrap) CalculateShiftChanges(s *Service, t time.Time) (rv time.Time) {
	if t.IsZero() {
		t = sw.Clock.Now()
	}
	s.shiftChanges = sw.ServiceRunningPeriods(s, t, false)
	s.shiftChangeIndex = -1
	// log.Printf("shiftchanges for %s at %s: %v", s.Name, t.Format(time.DateTime), s.shiftChanges)
	return
}

// NextShiftChange returns the index of the first ShiftChange in sc after
// t, or -1 if there is no such ShiftChange (i.e. if all ShiftChanges in sc are before
// t).
func NextShiftChange(t time.Time, scs ShiftChanges) (rv int) {
	if len(scs) == 0 {
		return -1
	}
	rv = sort.Search(len(scs),
		func(i int) bool {
			return scs[i].At.After(t)
		},
	)
	if rv == len(scs) {
		rv = -1
	}
	return
}

// AddShifts adds each shift in shs to service s.
// The service will be started if all of these are true:
//   - the service is being managed by shiftwrap
//   - the service is not running
//   - at least one of the new shifts includes the current time
//   - there is at least MinRuntime left before the new shift (or any subsequent shifts it overlaps)
//     ends
//
// If the service is started, any Setup command is first run through the shell.
func (sw *ShiftWrap) AddShifts(s *Service, shs ...*Shift) {
	if len(shs) == 0 {
		return
	}
	// add shifts
	for _, sh := range shs {
		sh.service = s
		s.Shifts[sh.Label] = sh
	}
	sw.ServiceChanged(s)
}

// DropShifts removes each shift named in shns from service s.
// The service will be stopped if:
// - the service is being managed by shiftwrap
// - the service is running and
// - the dropped shifts are the only shifts for this service which include the current time
// If the service is stopped, any Takedown script is then run through the shell
func (sw *ShiftWrap) DropShifts(s *Service, shns ...string) {
	if len(shns) == 0 {
		return
	}
	for _, shn := range shns {
		delete(s.Shifts, shn)
	}
	sw.ServiceChanged(s)
}

// ServiceChanged handles changes to a service.  If the
// service is not currently managed, nothing happens.
// If the service *is* currently managed, shift changes are recalculated,
// which might stop or start the service.
func (sw *ShiftWrap) ServiceChanged(s *Service) {
	if s.IsManaged {
		sw.ManageService(s, false)
		sw.ManageService(s, true)
	}
}

// DayStartEnd returns the start and end of the (local) day in which t occurs.
func DayStartEnd(t time.Time) (start, end time.Time) {
	start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	end = start.AddDate(0, 0, 1)
	return
}

// ShiftChangeComp compares two shiftchanges by time.
func ShiftChangeComp(a, b ShiftChange) int {
	return cmp.Compare(int64(a.At.UnixMicro()), int64(b.At.UnixMicro()))
}

// Sort shorts ShiftChanges by the .At field
func (scs ShiftChanges) Sort() {
	slices.SortFunc(scs, ShiftChangeComp)
}

// MergeOverlaps takes a slice of ShiftChange and merges any
// overlapping shifts.  This means it drops any of the original shift
// changes that occur within the newly-merged shifts.
// The For field of any remaining ShiftChange that was part of an
// overlap will be the same as one of the original shifts, but is not
// specified more precisely.
func (sw *ShiftWrap) MergeOverlaps(scs ShiftChanges) (rv ShiftChanges) {
	// shiftsStarted is a count of shifts
	shiftsStarted := 0
	// - if a shift starts but there are already other started shifts which haven't stopped, drop the start
	// - if a started shift stops but there are still other started shifts which haven't, drop the stop
	for _, sc := range scs {
		if sc.Type == Start {
			if shiftsStarted == 0 {
				// keep this start
				rv = append(rv, sc)
			}
			shiftsStarted++
		} else {
			shiftsStarted--
			if shiftsStarted == 0 {
				rv = append(rv, sc)
			}
		}
	}
	return
}

// RemoveShorts removes any shifts from scs which are less than MinRuntime
func (sw *ShiftWrap) RemoveShorts(scs ShiftChanges) (rv ShiftChanges) {
	if len(scs) == 0 {
		return
	}
	m := sw.MinRuntime(scs[0].Service())
	for _, sc := range scs {
		// if a shift is ending and we have the start shift
		// change, and if the shift is less than MinRuntime in
		// length, then drop the shift by removing the start
		// ShiftChange and don't add this stop ShiftChange
		// (we should always have the start ShiftChange if scs was created by ServiceRunningPeriods)
		if sc.Type == Stop && len(rv) > 0 && rv[len(rv)-1].Type == Start && sc.At.Sub(rv[len(rv)-1].At) < m {
			rv = rv[0 : len(rv)-1]
		} else {
			rv = append(rv, sc)
		}
	}
	return
}

// cachedSolarEvents keeps calculated solar event times for a sliding month window
// It is indexed by day of month.
var cachedSolarEvents = make([]SolarEvents, 31)

// GetSolarEvents returns the SolarEvents for a given date
func (sw *ShiftWrap) GetSolarEvents(t time.Time) (rv SolarEvents) {
	d := t.Day() - 1
	if rv = cachedSolarEvents[d]; rv == nil {
		cachedSolarEvents[d] = suncalc.GetTimesWithObserver(t, sw.Conf.Observer)
		rv = cachedSolarEvents[d]
	}
	return
}

// Time converts a TimeSpec to a time for the day given in t
func (sw *ShiftWrap) Time(t time.Time, ts *TimeSpec) (rv time.Time) {
	if ts.Origin == "" {
		rv = time.Date(t.Year(), t.Month(), t.Day(), ts.TimeOfDay.Hour(), ts.TimeOfDay.Minute(), ts.TimeOfDay.Second(), ts.TimeOfDay.Nanosecond(), time.Local)
	} else {
		se := sw.GetSolarEvents(t)
		if dt, okay := se[ts.Origin]; okay {
			rv = dt.Value.Add(ts.Offset)
		} else {
			log.Fatalf("unknown solar event: %s", ts.Origin)
		}
	}
	return
}

// ReadConfig reads .yml files from the confDir.  The file named
// `shiftwrap.yml` sets options for ShiftWrap, while any other
// file `X.yml` configures a Service.
// If confDir cannot be read, ReadConfig quits with a fatal error.
// If any .yml file fails to read or parse, an error is logged but
// ReadConfig attempts to read and parse remaining files.
func (sw *ShiftWrap) ReadConfig(confDir string) {
	dir, err := os.ReadDir(confDir)
	if err != nil {
		log.Fatalf("can't read shiftwrap configuration dir %s: %s", confDir, err.Error())
	}
	for _, de := range dir {
		if de.IsDir() {
			continue
		}
		p := path.Join(confDir, de.Name())
		var (
			buf []byte
			err error
		)
		if buf, err = os.ReadFile(p); err != nil {
			log.Printf("error reading shiftwrap config file %s: %s", p, err.Error())
			continue
		} else {
			if de.Name() == "shiftwrap.yml" {
				err = sw.Conf.Parse(buf)
			} else {
				s := &Service{}
				if err = s.Parse(buf); err == nil {
					sw.AddService(s)
					for _, sh := range s.Shifts {
						sh.service = s
					}
					if !strings.Contains(s.Name, "@") {
						s.IsRunning()
					}
				}
			}
		}
		if err != nil {
			log.Printf("error parsing config file %s: %s", p, err.Error())
		}
	}
}

// solarEventNames holds the lower-case names of all solar events in a
// map for fast verification
var solarEventNames = map[string]suncalc.DayTimeName{}

// solarEventNamesList is a formatted list of the solar event names
var solarEventNamesList string

func init() {
	sn := suncalc.DayTimeNames
	// For some reason, these two are missing from suncalc.DayTimeNames even though
	// present in suncalc.GetTimesWithObserver
	sn = append(sn, "nadir", "solarNoon")
	for n, s := range sn {
		solarEventNames[strings.ToLower(string(s))] = s
		if n > 0 {
			solarEventNamesList += ", "
		}
		solarEventNamesList += string(s)
	}
}

// SolarTimeSpecRx matches the format for a solar-event-based TimeSpec
var SolarTimeSpecRx = regexp.MustCompile(`^([a-zA-Z]+)[[:space:]]*(.*)$`)

// ParseTimeSpec returns a TimeSpec from a string.
// Syntax is
//
//	HH:MM:SS.SSS  for absolute time of day
//
// or
//
//	SOLAREVENT[DURATION] for an optional offset of DURATION relative to SOLAREVENT
//
// where SOLAREVENT is one of the (case-insensitive) strings:
//   - NightEnd (aka Astronomical Dawn)
//   - Night (aka Astronomical Dusk)
//   - Dawn (aka Civil Dawn)
//   - Dusk (aka Civil Dusk)
//   - GoldenHour
//   - GoldenHourEnd
//   - NauticalDawn
//   - NauticalDusk
//   - Sunrise
//   - SunriseEnd
//   - Sunset
//   - SunsetStart
//   - SolarNoon
//   - Nadir
//
// and DURATION is in the syntax understood by time.ParseDuration
//
// e.g. NauticalDawn -1h20m
func ParseTimeSpec(s string) (rv TimeSpec, err error) {
	var (
		d time.Duration
	)
	// check for absolute time of day
	if rv.TimeOfDay, err = time.ParseInLocation("15:04:05", s, time.Local); err == nil {
		return
	}
	if rv.TimeOfDay, err = time.ParseInLocation("15:04", s, time.Local); err == nil {
		return
	}
	err = nil
	parts := SolarTimeSpecRx.FindStringSubmatch(s)
	var evt, offset string
	if len(parts) > 1 {
		evt = parts[1]
		if len(parts) > 2 {
			offset = parts[2]
		}
	}
	evt = strings.ToLower(evt)
	var (
		found bool
	)
	if rv.Origin, found = solarEventNames[evt]; !found {
		err = fmt.Errorf("unknown solar event '%s'; must be one of %s", evt, solarEventNamesList)
		return
	}
	if offset != "" {
		d, err = time.ParseDuration(offset)
		if err != nil {
			err = fmt.Errorf("unable to parse time offset %s: %s", offset, err.Error())
			return
		}
		rv.Offset = d
	}
	return
}

// MustParseTimeSpec wraps ParseTimeSpec and exits the process if an error is returned.
// If there is no error, returns the address of the parsed TimeSpec.
func MustParseTimeSpec(s string) (rv *TimeSpec) {
	var (
		ts  TimeSpec
		err error
	)
	if ts, err = ParseTimeSpec(s); err != nil {
		log.Fatalf("can't parse timespec \"%s\": %s", s, err.Error())
	}
	rv = &ts
	return
}

// String formats a TimeSpec
func (t TimeSpec) String() (rv string) {
	if t.Origin == "" {
		rv = t.TimeOfDay.Format("15:04:05")
	} else {
		rv = string(t.Origin)
		if t.Offset > 0 {
			rv += "+"
		}
		if t.Offset != 0 {
			rv += t.Offset.String()
		}
	}
	return
}

// String formats a TidyDuration
// in the usual way but omitting zero components.
func (td TidyDuration) String() (rv string) {
	d := time.Duration(td)
	if d < 0 {
		rv = "-"
	}
	d = d.Abs()
	h := int(d.Hours())
	if h > 0 {
		rv += strconv.Itoa(h) + "h"
		d -= time.Duration(h) * time.Hour
	}
	m := int(d.Minutes())
	if m > 0 {
		rv += strconv.Itoa(m) + "m"
		d -= time.Duration(m) * time.Minute
	}
	if d > 0 {
		rv += strconv.FormatFloat(d.Seconds(), 'g', 3, 64) + "s"
	}
	return
}

// MarshalJSON formats a TidyDuration for JSON
func (td TidyDuration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + td.String() + `"`), nil
}

// String formats a TidyDuration using DurationCompactString

// Scheduler is run as a goroutine and is responsible for
// starting and stopping services on their schedules, and for
// handling requests to manage or unmanage services.
func (sw *ShiftWrap) Scheduler() {
	confmsg := SchedMsg{Type: SMConfirm}
	for {
		select {
		case et := <-sw.scTimer.Chan():
			// log.Printf("scTimer triggered at %s\n", et.Format(time.StampMicro))
			sc := sw.scQueue.Head()
			if sc == nil {
				// log.Printf("weird - no shift changes left in queue\n")
			} else {
				tmpsc := *sc
				tmpsc.trueAt = et
				for _, scl := range sw.shiftChangeListeners {
					go func(c chan ShiftChange) {
						c <- tmpsc
					}(scl)
				}
			}
			sw.doShiftChange(et)
		case msg := <-sw.schedChan:
			// log.Printf("Scheduler got message %#v\n", msg)
			switch msg.Type {
			case SMQuit:
				sw.schedChan <- confmsg
				return
			case SMManageService:
				sw.doManageService(msg.Service)
				sw.schedChan <- confmsg

			case SMUnmanageService:
				sw.doUnmanageService(msg.Service)
				sw.schedChan <- confmsg
			}
		}
		next := sw.doEnsureTimer()
		if next.IsZero() {
			// no shift change scheduled, so there's nothing to do, but we don't know how
			// long this will last, so can't jump to the future or idle
			continue
		}
		// There are two situations where we don't just wait for a timer or message:
		//
		// 1. sw.Hurry is set, which means skip all waits; this is intended for testing,
		//    as an alternative to a highly dilated clock.
		//
		// or
		//
		// 2. an idle handler is set, and no Services are running (i.e. all are waiting
		//    for a start ShiftChange)
		//
		if sw.Hurry {
			sw.Clock.JumpToFutureTime(next)
		} else if sw.NumManagedServices > 0 && sw.numRunningServices == 0 {
			sw.doIdle(next)
		}
	}
}

// doIdle runs the idle handler if it is set-up and the idle period is sufficiently long and the
// ShiftWrap has been running for long enough (to allow all boot-time `systemctl start shiftwrap@X`
// events to have occurred).
func (sw *ShiftWrap) doIdle(next time.Time) {
	now := sw.Clock.Now()
	realNow := time.Now()
	left := next.Sub(now)
	// log.Printf("%s: got to idle with left=%d until=%s", now.Format(time.DateTime), left/1e9, next.Format(time.DateTime))
	if sw.Conf.IdleHandlerCommand != "" && realNow.Sub(sw.startTime) >= sw.Conf.IdleHandlerInitialDelay && sw.Clock.RealDuration(left) >= sw.Conf.IdleHandlerMinRuntime {
		cmd := exec.Command(sw.Conf.Shell, "-c", sw.Conf.IdleHandlerCommand)
		cmd.Env = append(cmd.Environ(),
			fmt.Sprintf("SHIFTWRAP_IDLE_DURATION=%d", sw.Clock.RealDuration(left)/1e9),
			fmt.Sprintf("SHIFTWRAP_IDLE_DURATION_DILATED=%d", sw.Clock.RealDuration(left)/1e9),
			fmt.Sprintf("SHIFTWRAP_NEXT_EVENT_TIME=%d", sw.Clock.RealTime(next).UnixNano()/1e9),
			fmt.Sprintf("SHIFTWRAP_IDLE_DURATION_DILATED=%d", sw.Clock.RealTime(next).UnixNano()/1e9),
		)
		if output, err := cmd.Output(); err != nil {
			log.Printf("error in idle handler %s: %s", cmd.String(), err.Error())
		} else {
			log.Printf("idle handler got output: %s", output)
		}
	}
}

// doEnsureTimer makes sure that the ShiftChange timer is waiting for
// the first ShiftChange in the queue, if any.  It returns the time
// of this ShiftChange, or the zero time.
func (sw *ShiftWrap) doEnsureTimer() (rv time.Time) {
	// log.Printf("got to doEnsuretimer\n")
	if sw.scQueue.Len() == 0 {
		// log.Printf("empty shift queue so stopping timer\n")
		select {
		case <-sw.scTimer.Chan():
		default:
			sw.scTimer.Stop()
		}
	} else {
		head := sw.scQueue.Head()
		if head.id == sw.prevHeadID {
			// nothing to do
			return
		}
		sw.prevHeadID = head.id
		to := head.At
		// log.Printf("current timer is %v; to=%v\n", sw.scTimer.Target(), to)
		if to.Sub(sw.Clock.Now()) > time.Millisecond {
			sw.scTimer.ResetTo(to)
			// log.Printf("scTimer reset to %s\n", to.Format(time.StampMicro))
		} else {
			// log.Printf("shift change is in the past, reset timer to immediate future\n")
			sw.scTimer.Reset(time.Millisecond)
			to = sw.scTimer.Target()
		}
		rv = to
	}
	return
}

// doShiftChange performs a shift change:
//   - removes the first ShiftChange from the queue (this is the one
//     whose .At time ShiftWrap has finished waiting for)
//   - starts or stops the relevant Service
//   - advances the Service's shift index, calculating a new day of shifts
//     if necessary
func (sw *ShiftWrap) doShiftChange(now time.Time) {
	// pop the first ShiftChange
	// perform the ShiftChange
	// if there are no shiftChange left in the heap for this service,
	// calculate later ones.  If there are no further shift changes (in the next
	// year), stop managing this service.
	sc, ok := sw.scQueue.PopFirst()
	if !ok {
		log.Printf("weird - no shift changes left and numManagedServices = %d\n", sw.NumManagedServices)
		return
	}
	s := sc.Service()
	if sc.Type == Start {
		// log.Printf("starting shift %s at %v", sc.Shift().Label, sw.Clock.Now())
		sw.Start(s, sc.shift, now)
	} else {
		// log.Printf("stopping shift %s at %v", sc.Shift().Label, sw.Clock.Now())
		sw.Stop(s, sc.shift, now)
	}
	if !sw.advanceShiftChange(s, now) {
		log.Printf("stopping management of service %s", s.Name)
		sw.doUnmanageService(s)
	}
}

// advanceShiftChange updates the service's shift change index, calculating
// new shift changes if at the end of the shift change list.  It will look for up to
// a year for shifts, printing an error and returning false if no shift is found.
// Otherwise, returns true.
func (sw *ShiftWrap) advanceShiftChange(s *Service, now time.Time) bool {
	s.shiftChangeIndex++
	// log.Printf("shift change index for %s: %d, len=%d\n", s.Name, s.shiftChangeIndex, len(s.shiftChanges))
	if s.shiftChangeIndex >= len(s.shiftChanges) {
		// this was the last ShiftChange for this Service in the heap
		// log.Printf("recalculating shift changes")
		t := now
		var (
			scn int
		)
		// loop until a future shift change is found, but no more
		// than a year into the future
		for days := 0; days <= 365; days++ {
			if days == 365 {
				log.Printf("no shift changes for service %s in the year after %s; possible error in Shift specifications?", s.Name, now.Format(time.DateTime))
				return false
			}
			sw.CalculateShiftChanges(s, t)
			scn = NextShiftChange(now, s.shiftChanges)
			// log.Printf("next shift change index for %s is %d", s.Name, scn)
			if scn >= 0 {
				// we have a valid next shift change so add it and subsequent ones to the heap
				sw.scQueue.Add(s.shiftChanges[scn:]...)
				break
			}
			// unusual, but possible; e.g. a Shift.TimeSpec is relative
			// to a solar event which doesn't happen on a given calendar day
			// e.g. sunrise above the Arctic Circle in summer
			t = t.Add(time.Duration(24 * time.Hour))
		}
		s.shiftChangeIndex = scn
		// log.Printf("at %v, new shiftchange index is %d, new shiftchanges are %v", sw.Clock.Now(), scn, s.shiftChanges)
	}
	return true
}

// doManageService begins management of service s
// This enqueues the shift schedule for the current day,
// and also checks whether Service should already be running
// (because management is starting during a shift) or already not
// running (because management is starting outside of any shifts),
// and starts/stops Service accordingly.
func (sw *ShiftWrap) doManageService(s *Service) {
	// calculate shift changes for this service and add to heap
	if s.IsManaged {
		return
	}
	now := sw.Clock.Now()
	if !sw.advanceShiftChange(s, now) {
		log.Printf("refusing to manage service %s", s.Name)
		return
	}
	s.IsManaged = true
	sw.NumManagedServices++
	// s.shiftChanges contains starts and stops for any shifts
	// that overlap the current day, in particular the current time.
	// s.shiftChangeIndex indicates the next ShiftChange, so
	// its IsStart value will be the opposite of what the current running
	// state *should* be.
	sc := s.shiftChanges[s.shiftChangeIndex]
	waitingForStart := sc.Type == Start
	// if s is not waiting for a Start, then s is waiting for a Stop,
	// and so should be running.
	if !waitingForStart && !s.running {
		if s.shiftChangeIndex == 0 {
			// sanity check fail:  we are waiting for a Stop ShiftChange, but there is no
			// preceding Start ShiftChange.
			log.Printf("management of %s started during unknown shift; not doing initial Start", s.Name)
			return
		}
		// normal case, the preceding shift change started the shift
		// we are in the middle of, so start that shift, provided there is
		// enough runtime left
		if sc.At.Sub(now) >= sw.MinRuntime(s) {
			sw.Start(s, s.shiftChanges[s.shiftChangeIndex-1].shift, now)
		}
	} else if waitingForStart && s.running {
		// service should not be running.  Try to find what shift would have started it if it
		// had been under shiftwrap management, and stop that shift.
		var sh *Shift
		if s.shiftChangeIndex > 0 {
			sh = s.shiftChanges[s.shiftChangeIndex-1].shift
		} else {
			// no shift that overlaps today would have started Service,
			// so look at yesterday's shiftChanges
			scs := sw.ServiceRunningPeriods(s, now.Add(-24*time.Hour), false)
			// find the last Start shiftChange yesterday
			for i := len(scs) - 1; i >= 0; i-- {
				if scs[i].Type == Start {
					sh = scs[i].shift
					break
				}
			}
			sw.Stop(s, sh, now)
		}
	}

}

func (sw *ShiftWrap) doUnmanageService(s *Service) {
	sw.scQueue.Remove(s)
	s.IsManaged = false
	s.shiftChanges = s.shiftChanges[0:0]
	sw.NumManagedServices--
}
