# Shiftwrap

**Note:  this is a work in progress.  A statically-configured `/etc/shiftwrap` folder will work,
but the live API via `shiftwrapd` is still in progress.  Completion target is July 31, 2025.**

`Shiftwrap` wraps `systemd` services to ensure they are running during one or more
daily shifts.  Shift start and stop times can be fixed times of day, or
relative to solar events such as sunrise.  Each shift can have start-up
or take-down code that can be used to make the service run differently than
in other shifts, e.g. by modifying a config file.

When shiftwrap is used to wrap a service, that service will run during all
portions of its defined shifts for which the computer is up, so if the
computer boots into the middle of a service shift, the service will be started
immediately and stopped at the end of that shift.

Optionally, shiftwrap can be configured to run a command when none of
the wrapped services are running.  A special case of this is to sleep
the system between service shifts; informally: *when no wrapped
service is running a shift, sleep*.  This can be useful for energy-constrained settings.

Shifts for your wrapped service `X` are defined in the file `/etc/shiftwrap/X.yml`
(see below for the syntax). You can send commands to a running `shiftwrapd` to define new
services.

## Usage:

To control a shiftwrapped service, use `systemctl` commands, but with your
service as the parameter to the `shiftwrap@` service template.  If
your service is also instantiated, the instance name will have more than one `@`:  the
first to separate `shiftwrap` from your service's instance name, the second
to separate your service name from its instance name;  e.g. if you want shiftwrap to control the
service `foo@tty0`, then use the service name `shiftwrap@foo@tty0` in
the commands described below.

These commands start/stop/enable/disable **control of your service by
shiftwrap**, rather than your service itself.  When under shiftwrap's
control, your service is started/stopped on its shift schedule; when
not under shiftwrap's control, your service is started/stopped
directly by systemd (or manually by you) according to the usual rules.
When shiftwrap control is enabled for a service, that service is
placed under shiftwrap control after each reboot.

### Enabling shiftwrap control of your service
```
systemctl enable [--now] shiftwrap@foo
```
After the next reboot, your service will be started/stopped by shiftwrap, according to the shifts defined in `/etc/shiftwrap/foo.yml`.
If your service was already enabled directly with `systemctl`, you must first
disable it with `systemctl disable foo`.  If you specify the `--now` option, then this command also does `systemctl start shiftwrap@foo` (see below).

### Disabling shiftwrap control of your service
```
systemctl disable shiftwrap@foo
```
After the next reboot, your service will not be started or stopped by shiftwrap.  You can of course
cause your service to be started directly by systemd by doing `systemctl enable foo`.  You can also
have shiftwrap manage your service only for the current boot session by doing `systemctl start shiftwrap@foo` (see below0.

### Starting shiftwrap control of your service
```
systemctl start shiftwrap@foo
```

This immediately puts your service `foo` under shiftwrap control.
Shiftwrap will check whether the current time is within a shift
defined in `/etc/shiftwrap/services/foo.yml`, and if so, it will start `foo`.  Conversely, if
`foo` is already running and the current time is *not* within a shift
for `foo`, then `foo` will be immediately stopped.  If `foo`'s current
running state is already correct given its shift schedule, then `foo`
will be neither stopped nor started until the next shift change.
Unless you have also done `systemctl enable shiftwrap@foo`, then after
the next reboot, your service will no longer be controlled by
shiftwrap.

### Stopping shiftwrap control of your service
```
systemctl stop shiftwrap@foo
```
This removes your service `foo` from shiftwrap's control. `foo`'s running state will be left
as-is, but shiftwrap will not start or stop it again.  However, if you have done `systemctl enable shiftwrap@foo`,
then after the next boot, your service `foo` will again be controlled by shiftwrap.

## Configuration

  - shifts for your service `X` are defined in the file `$SHIFTWRAP_DIR/services/X.yml`,
   where `SHIFTWRAP_DIR` can be defined in the environment passed to shiftwrap,
   and defaults to `/etc/shiftwrap` if empty or not defined.

  - global configuration for `shiftwrap` can be provided in the file
    `$SHIFTWRAP_DIR/shiftwrap.yaml`

  - an `X.yaml` defines one service, and looks like:
```yaml
name: NAME
issystemd: true/false
minruntime: DURATION
shifts:
  LabelForShift1:
    setup: SHELL_COMMANDS
    start: TIMESPEC
    stop: TIMESPEC
    mustinclude: TIMESPEC
    mustexclude: TIMESPEC
    takedown: SHELL_COMMANDS
  LabelForShift2:
    setup: SHELL_COMMANDS
    start: TIMESPEC
    stop: TIMESPEC
    mustinclude: TIMESPEC
    mustexclude: TIMESPEC
    takedown: SHELL_COMMANDS
  ...
```
- the Service must have a unique `Name`, which is used to refer to the service in API calls.  This does not have to
be the same as the filename.
- if `issystemd` is `true`, then `Name` is the name of the systemd service; i.e. `X` .  The matching is case-sensitive.
- if `issystemd` is `false`, no systemd service is started/stopped, but `setup` and `takedown`
scripts are run at the appropriate times.  This allows use of `shiftwrap` to control
things other than systemd services.
- each shift must have a `start` and `stop` field, which can relate to a solar event - see below.

- `setup` and `takedown` fields are optional; they provide shell
commands to run before starting and after stopping service `X`; they
are only used for the shift(s) where they are specified.  They allow
you to run service `X` differently in each shift by, e.g. changing
values in a configuration file for `X`.  Shiftwrap defines these
environment variables when running `setup` and `takedown` commands:
  - `SHIFTWRAP_SERVICE`: name of the service; this can be useful for instantiated services because it includes the instance name, if any; (e.g. `EatSerial@ttyS0`)
  - `SHIFTWRAP_SHIFT`: label for the shift
  - `SHIFTWRAP_TIME`: time of the shift change, according to the shiftwrap clock; normally,
  this is the current system time, but if shiftwrap was run with an
  alternative clock (e.g. by using the `-clockdilate` and
  `-clockepoch` flags to `shiftwrapd`), then that clock's time is
  available here.  The time is formatted as integer seconds since the
  Unix epoch.
  - `SHIFTWRAP_ACTION`: the word `setup` or `takedown`
- `minruntime` is optional; it is the shortest length of a shift (or
remainder of a shift, if booting into a shift) for which the service
`X` will be run.  This permits skipping short shifts, which might be
desirable if the `setup` or `takedown` commands take significant time
to run, or if service `X` isn't useful when run for less than a
certain duration (e.g. a service that reads from a sensor whose
sampling period is a minute would not be useful to run for less than
that).  `minruntime` defaults to 100 ms, to allow for a bit of slop
when booting into a shift.  The main cause of varying
shift lengths is defining `start` and/or `stop` times relative
to solar events (e.g. a shift from `sunrise-1h` to `sunset+1h`).

  - syntax for `minruntime` (and time offsets, see below) is that of golang's
    [time.Duration.ParseDuration function](https://pkg.go.dev/time@go1.23.1#ParseDuration),
	namely:

```

A duration string is a possibly signed sequence of decimal numbers,
each with optional fraction and a unit suffix, such as "300ms",
"-1.5h" or "2h45m". Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
```
    Note that only a single, leading sign is permitted; e.g. "1h-2m" is not valid.

- Start/Stop times can each be specified as fixed local times in 24hr
format (e.g. HH:MM:SS.SSS), or as times relative to solar events

- solar events are those known to the suncalc R and go packages:
  - `sunrise`: The moment when the upper edge of the solar disk
  becomes visible above the horizon
  - `sunset`: The moment when the upper edge of the solar disk
  disappears below the horizon
  - `dawn`: The moment when the geometric centre of the Sun reaches
  6° below the horizon as it is rising. (also known as civil dawn)
  - `dusk`: The moment when the geometric centre of the Sun
            reaches 6° below the horizon as it is setting. (also known
            as civil dusk)

  - `nauticalDawn`: The moment when the geometric centre of the
  Sun reaches 12° below the horizon as it is rising

  - `nauticalDusk`: The moment when the geometric centre of the
  Sun reaches 12° below the horizon as it is setting

  - `nightEnd`: The moment when the geometric centre of the Sun
  reaches 18° below the horizon as it is rising.  (also known as
  astronomical dawn)

  - `night`: The moment when the geometric centre of the Sun
  reaches 18° below the horizon as it is setting (also known as
  astronomical dusk)

  - `solarNoon`: The moment when the Sun reaches its highest
  point in the sky.

  - `nadir`: darkest moment of the night, sun is in the lowest
  position, at which time it will usually not be visible

  - `goldenHourEnd`: morning golden hour (soft light, best DayTime for photography) ends

  - `goldenHour`: evening golden hour starts

  - `sunriseEnd`: bottom edge of the sun touches the horizon

  - `sunsetStart`: bottom edge of the sun touches the horizon

  - an optional offset relative to a solar event can be given by an expression
    with the same syntax as `MinRuntime` (see above)

## Example
Running this shell command:
```sh
    systemctl start shiftwrap@amcam
```
with the following lines in `/etc/shiftwrap/services/amcam.yaml`:
```yaml
shifts:
  overnight:
    start: sunset-1h
    stop:  sunrise+1h
  lunchtime:
    start: solarnoon-1h
    setup: /usr/bin/don_shades
    stop:  solarnoon+1h
    takedown: /usr/bin/lose_shades
  afternoon quickie:
    start: 15:00
    stop:  15:30
```
will ensure that the systemd amcam service is running between 1 hour
before sunset and 1 hour after sunrise each day, between 1 hour
before noon and 1 hour after noon each day, and from 3 - 3:30 pm
localtime each day.  Moreover, the command `don_shades` will be
run before starting X at 1 hour before solar noon, and the command
`lose_shades` will be run after stopping X at 1 hour after noon.
The `don_shades` and `lose_shades` commands are *not* run
as part of the other two shifts.
This pattern of running `amcam` will *not* persist across reboots unless
you also run this shell command:
```sh
   systemctl enable shiftwrap@amcam
```

## Global Shiftwrap Configuration
Additional configuration options can be set in the file `$SHIFTWRAP_DIR/shiftwrap.yaml`:

### Location parameters
These are used to calculate solar event times, and are required if any
`Start` or `Stop` fields refer to a solar event:
```yaml
- Latitude: LAT
- Longitude: LON
- Altitude: ALT  # defaults to 0
```

### Other parameters
Default values across all services for `min_runtime` and `shell`:
```yaml
- DefaultMinRuntime: DURATION
- DefaultShell: PATH_TO_SHELL
```

## Notes:

- shift `start` and `stop` times are calculated each day, if necessary
  (i.e. if they depend on solar events)

- if a shift has start TIMESPEC earlier than stop TIMESPEC (both
  calculated today), that shift is entirely within "today" (in local
  time).

- if a shift has start TIMESPEC later than stop TIMESPEC (both
  calculated today), that shift is treated as running overnight, and
  the stop TIMESPEC is recalculated for the following day (if
  needed; i.e. if based on a solar event).

- for any set of overlapping shifts, the shifts are effectively merged
  into a larger shift:
	  - the service is run without restarts from the start of the earliest
	    overlapping shift to the end of the latest overlapping shift
	  - only the `setup` (if any) for the first overlapping shift and
        the `takedown` (if any) for the last one are run; i.e. `setup`
		and `takedown` inside the merged shift are not run.

- `shiftwrap` aims for good accuracy of shift timing; depending on your system,
  service `X`should be started and stopped within 1 second or less of the
  nominal shift times.

- any setup commands are run synchronously before the service `X` is
  started, so if these take a signficant amount of time, you should
  make the start time earlier to ensure service `X` starts when you want
  it to.

For the `amcam` example above, the sunset is calculated today, but the
sunrise is calculated tomorrow, so that the first running shift is
from 1 hour before today's sunset to 1 hour after tomorrow's
sunrise.

## Files

`shiftwrapd`: daemon, run from `shiftwrapd.service`; must be enabled and started in
order to use shiftwrap.  Controlled via an in-process http server.

`shiftwrapd.service`: describes the `shiftwrapd` daemon

`sw`: command-line client that communicates with `shiftwrapd` via
http; it is used by `shiftwrap@.service` to start and stop shiftwrap
control of services.

`X.service`: user-defined systemd service files (not part of
shiftwrap, but used by it).  These can be templates.  To use an
instantiated unit with shiftwrap, use `your-unit-name@your-instance`
as the instance name for shiftwrap@.  e.g.  to have `shiftwrap`
control service `read-serial@` with parameter `ttyS0`, do:

```
	systemctl start shiftwrap@read-serial@ttyS0
```
In this case, `shiftwrap` will do `systemctl stop read-serial@ttyS0` or `systemctl start read-serial@ttyS0`
to start and stop the service.

## Shiftwrapd API
Shiftwrapd runs an HTTP server to let you query and control it.
Requests and responses are in JSON format (and so must have the HTTP header `Content-type: application/json`)
Here's the API:

`
/config
`

- **GET**: Return the shiftwrapd configuration.  These are items whose defaults are read from `shiftwrap.xml` at startup.
- **PUT**:
`
/time
`

- **GET**: Return the current time, according to the shiftwrap clock.  This will differ from the system time
if `shiftwrapd` was run with `-clockdilate` and/or `clockepoch` options

`
/timer
`

- **GET**: return the timer target; this is when the next shift change will happen.  The value is according
to the `shitwrapd` clock (see `\time` agove)

`
/queue
`

- **GET**: return the queue of upcoming shift changes for all services, out to approximately one day in the future.

`
/services
`

- **GET**: return an array of names of services currently managed by `shiftwrapd`

`
/service/{sn}
`

- **GET**: return the definition of service `sn`; `sn` can be the name of a service defined in the file `$SHIFTWRAP_DIR/services/sn.yml`, or the name of a service defined by a previous **PUT** request to this path.
- **PUT**: create or modify the definition of service `sn`; this can inlude the `IsManaged` boolean property, which enables or disables management of the service by `shiftwrapd`.  If the service is already being managed, or if `IsManaged` is specified as `true`, then any other fields specified take effect immediately, which can lead to recalculation of shift changes and/or stopping or starting service `sn`.

Note:  For this and other APIs, `{sn}` can be an instantiated service name, e.g. `EatSerial@ttyS0` provided the portion of the name up to and including the first `@` is the name of an already-defined service (e.g. `EatSerial@`).

`
/service/{sn}/shiftchanges
`

- **GET**: return an array of recent and upcoming shift changes for service `sn`; this will include (at least) shift changes for shifts that overlap the current day.  These are *cooked* shift changes:  overlapping shifts have been merged, and any shifts shorter than the service's `minruntime` property have been removed.

`
/service/{sn}/shiftchanges/{date}
`

- **GET**: return an array of shift changes for service `sn` arising from shifts that overlap the given date.  These are *cooked* shift changes:  overlapping shifts have been merged, and any shifts shorter than the service's `minruntime` property have been removed.

`
/service/{sn}/shiftchanges/{date}/raw
`

- **GET**: returns an array of shift changes of service `sn` arising from shifts that overlap the given `date`.  These are *raw* shift changes:  they might include overlaps and shifts shorter than the service's `MinRuntime` property.

`
/service/{sn}/shiftchanges/{date}
`

- **GET**: return an array of shift changes for service `sn` arising from shifts that overlap the given date.  These are *cooked* shift changes:  overlapping shifts have been merged, and any shifts shorter than the service's `minruntime` property have been removed.

## Shift Time Semantics

Before describing shiftwrap's algorithm for calculating shifts, here are some examples of shift definitions where the user's intention seems clear, but for which there are locations and dates that seem to misbehave.

### Same Day
```
   start: sunrise+1h
   stop: 11:30
```

- **Intended Behaviour**: The shift runs from 1 hour after sunrise today until 11:30 am today.
- **Anomaly**: If sunrise is 10:45, then `start` is 11:45 but `stop`
is 11:30.  This could mean either an empty shift, or a very long shift
running from yesterday's `start` time to today's `stop` time, as for
an overnight shift.

### Overnight
```
   start: dusk+3h
   stop: dawn-3h
```

- **Intended Behaviour**: The shift runs from 3 hours after dusk today until 3 hours before dawn tomorrow.
- **Anomaly**: If the period between dusk today and dawn tomorrow is
  less than 6 hours, then the `stop` time from tomorrow is before the
  `start` time from today.

## The Shift Change Algorithm

Running periods for a service S are calculated for date D like so:
 - each shift definition is allowed to overlap D in at most two distinct
   intervals (e.g. an overnight shift will have both an early morning and late
   night overlap)
 - shift definitions are assumed to be reasonably well-behaved: the shift
   change time.Times calculated from a shift definition for today are all assumed to be
   later than those calculated for yesterday, and earlier than those calculated for
   tomorrow (calculated shift change times do not have to occur on the date for which they
   are calculated, though; e.g. `sunset+6h` might be a time after midnight)
 - let T1 = times computed for `D` and following days until reaching a day which does not add a running period that overlaps `D`
 - let T2 = times computed for `D-1` and preceding days until reaching a day which does not add a running period that overlaps `D`
 - let T3 = the union of T1 and T2, sorted in increasing order of time
 - calculate running periods from T3 according to the sub-algorithm below
 - merge running periods from all shift definitions
 - remove running periods which are too short or which don't overlap date D

#### Sub-algorithm: running periods for a shift

For the times in `T3` defined above:
 - let `On` = be the times calculated from `X.start`
 - let `Off` = be the times calculated from `X.stop`
 - let `In` = be the times calculated from `X.mustinclude`, if present
 - let `Out` = be the times calculated from `X.mustexclude`, if present
 - let R = all intervals starting at a time in `On` and ending at the the first time in `Off` which comes later.
 - if the shift has a `mustinclude` field, remove from R any intervals which do not contain a time in `In`
 - if the shift has a `mustexclude` field, remove from R any intervals which contain a time in `Out`
 - what remains in `R` is the set of runnning periods for the shift.

### Preventing Anomalous Shifts with MustInclude and MustExclude

You can use the `MustInclude` and `MustExclude` fields to prevent
anomalous shifts.  These fields can preserve the `Same-Day` or
`Overnight` character of the shift, but also permit other kinds of
filtering.

### Same-Day without Anomalies

```
   start: sunrise+1h
   stop: 11:30
   mustexclude: 00:00
```

- **Anomaly**: sunrise+1h is later than 11:30
- **Anomalous Running Period**: from sunrise+1h today until 11:30 tomorrow
- **Avoided because**: that running period would include 00:00 (midnight), which is forbidden by the `mustexclude` field.

### Overnight Without Anomalies
```
   start: dusk+3h
   stop: dawn-3h
   mustinclude: 00:00
```

- **Anomaly**: the time between dusk today and dawn tomorrow is less than 6 hours
- **Anomalous Running Period**: early today (i.e. dusk yesterday + 3h) to late today (i.e. dawn tomorrow - 3h)
- **Avoided because**: that running period would not include 00:00 (midnight), which is mandated by the `mustinclude` field.
