package shiftwrap

import (
	"fmt"
	"testing"
	"time"

	"github.com/jbrzusto/timewarper"
	"github.com/sixdouglas/suncalc"
)

var (
	Obs            suncalc.Observer
	SW             *ShiftWrap
	T1             time.Time
	TestService    *Service
	TestService2   *Service
	TestService3   *Service
	TestShifts     NamedShifts
	TestShifts2    NamedShifts
	TestShifts3    NamedShifts
	TestShifts4    NamedShifts
	TestShiftNamed = map[string]*Shift{}
)

func init() {
	Obs = suncalc.Observer{Latitude: 45, Longitude: -65, Height: 0, Location: time.Local}
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	T1 = time.Date(2025, 04, 24, 12, 0, 0, 0, time.Local)
	TestService = &Service{Name: "bogus", IsSystemd: false, MinRuntime: 5 * time.Minute}
	TestShifts = NamedShifts{
		"overnight": &Shift{
			service:  TestService,
			Label:    "overnight",
			Start:    MustParseTimeSpec("sunset+10m0.2s"),
			Stop:     MustParseTimeSpec("sunrise-15m"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for overnight shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for overnight shift of bogus service",
		},
		"mid-day": &Shift{
			service:  TestService,
			Label:    "mid-day",
			Start:    MustParseTimeSpec("solarnoon-1h"),
			Stop:     MustParseTimeSpec("solarnoon+3h"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for mid-day shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for mid-day shift of bogus service",
		},
		"afternoon": &Shift{
			service:  TestService,
			Label:    "afternoon",
			Start:    MustParseTimeSpec("15:00"),
			Stop:     MustParseTimeSpec("16:30"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for afternoon shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for afternoon shift of bogus service",
		},
		"beforetoday": &Shift{
			service:  TestService,
			Label:    "beforetoday", // this shift should never cause shift changes "today"
			Start:    MustParseTimeSpec("solarnoon-16h"),
			Stop:     MustParseTimeSpec("solarnoon-14h"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for beforetoday shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for beforetoday shift of bogus service",
		},
		"aftertoday": &Shift{
			service:  TestService,
			Label:    "aftertoday", // this shift should never cause shift changes "today"
			Start:    MustParseTimeSpec("solarnoon+14h"),
			Stop:     MustParseTimeSpec("solarnoon+16h"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for aftertoday shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for aftertoday shift of bogus service",
		},
		"mini1": &Shift{
			service:  TestService,
			Label:    "mini1", //
			Start:    MustParseTimeSpec("17:02"),
			Stop:     MustParseTimeSpec("17:03"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for mini1 shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for mini1 shift of bogus service",
		},
		"mini2": &Shift{
			service:  TestService,
			Label:    "mini2", //
			Start:    MustParseTimeSpec("17:02:30"),
			Stop:     MustParseTimeSpec("17:03:30"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for mini2 shift of bogus service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for mini2 shift of bogus service",
		},
	}
	for _, tsh := range TestShifts {
		TestShiftNamed[tsh.Label] = tsh
	}
	TestService2 = &Service{Name: "fake-o", IsSystemd: false, MinRuntime: 30 * time.Minute}
	TestShifts2 = NamedShifts{
		"overnight2": &Shift{
			service:  TestService2,
			Label:    "overnight2",
			Start:    MustParseTimeSpec("sunset-15m"),
			Stop:     MustParseTimeSpec("sunrise+20m"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for overnight2 shift of fake-o service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for overnight2 shift of bogus2 service",
		},
		"mid-day2": &Shift{
			service:  TestService2,
			Label:    "mid-day2",
			Start:    MustParseTimeSpec("sunrise+5h"),
			Stop:     MustParseTimeSpec("solarnoon+15m"),
			Setup:    "echo @`date --date @$SHIFTWRAP_TIME` doing Setup for mid-day2 shift of fake-o service",
			Takedown: "echo @`date --date @$SHIFTWRAP_TIME` doing Takedown for mid-day2 shift of fake-o service",
		},
	}
	for _, tsh := range TestShifts2 {
		TestShiftNamed[tsh.Label] = tsh
	}
	TestService3 = &Service{Name: "whammo", IsSystemd: false, MinRuntime: 1 * time.Minute}
	TestShifts3 = NamedShifts{
		"morning": &Shift{
			service:  TestService3,
			Start:    MustParseTimeSpec("dawn+1h"),
			Stop:     MustParseTimeSpec("solarNoon+2h"),
			Setup:    "echo setting up for morning whammo",
			Takedown: "echo taking down whammo",
		},
	}
	TestShifts4 = NamedShifts{
		"morning_wonky": &Shift{
			service:     TestService3,
			Label:       "morning",
			Start:       MustParseTimeSpec("dawn+4.5h"),
			Stop:        MustParseTimeSpec("solarNoon-3h"),
			MustExclude: MustParseTimeSpec("00:00"),
			Setup:       "echo setting up for morning wonky whammo",
			Takedown:    "echo taking down wonky whammo",
		},
	}
	for _, tsh := range TestShifts3 {
		TestShiftNamed[tsh.Label] = tsh
	}
	for _, tsh := range TestShifts4 {
		TestShiftNamed[tsh.Label] = tsh
	}
	SW.Quit()
}

func TestParseTimeSpecValidSolar(t *testing.T) {
	s := "nauticalDawn -1h20m"
	want := TimeSpec{Origin: suncalc.NauticalDawn, Offset: time.Duration(-80 * time.Minute)}
	ts, err := ParseTimeSpec(s)
	if err != nil || ts != want {
		t.Errorf(`ParseTimeSpec("%s") = %q, %v, want %#q, nil`, s, ts, err, want)
	}
}

func TestParseTimeSpecValidTOD(t *testing.T) {
	s := "12:34:56.001"
	// TimeSpec stores TimeOfDay as a time on Jan 1, year 0.
	want := TimeSpec{TimeOfDay: time.Date(0, 1, 1, 12, 34, 56, 1e6, time.Local)}
	ts, err := ParseTimeSpec(s)
	if err != nil || ts != want {
		t.Errorf(`ParseTimeSpec("%s") = %q, %v, want %#q, nil`, s, ts, err, want)
	}
}

func TestParseTimeSpecBad(t *testing.T) {
	s := "noSuchEvent"
	ts, err := ParseTimeSpec(s)
	if (ts != TimeSpec{}) || err == nil {
		t.Errorf(`ParseTimeSpec("%s") = %q, %v, want "", error`, s, ts, err)
	}
}

func TestTimeFromTimeSpec(t *testing.T) {
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	ts := TimeSpec{Origin: "sunrise", Offset: -2*time.Hour - 5*time.Minute}
	// results from the R suncalc package
	want := time.Date(2025, 04, 24, 06, 21, 02, 0, time.Local).Add(-2*time.Hour - 5*time.Minute).Truncate(time.Second)
	nt := SW.Time(T1, &ts).Truncate(time.Second)
	if !nt.Equal(want) {
		t.Errorf(`rounded ShiftWrap.Time(%q, %q) = %q, want %q`, T1, ts, nt, want)
	}
	SW.Quit()
}

func TestGetSolartEvents(t *testing.T) {
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	ev := SW.GetSolarEvents(T1)
	// results from	R package suncalc with this R script:
	// > t = suncalc::getSunlightTimes(as.Date("2025-04-24"), lat=45, lon=-65, tz="America/Halifax")
	// > structure(cbind(t(t), names(t)), dimnames=NULL)
	//       [,1]                  [,2]
	//  [4,] "2025-04-24 13:19:14" "solarNoon"
	//  [5,] "2025-04-24 01:19:14" "nadir"
	//  [6,] "2025-04-24 06:21:02" "sunrise"
	//  [7,] "2025-04-24 20:17:25" "sunset"
	//  [8,] "2025-04-24 06:24:14" "sunriseEnd"
	//  [9,] "2025-04-24 20:14:13" "sunsetStart"
	// [10,] "2025-04-24 05:49:27" "dawn"
	// [11,] "2025-04-24 20:49:00" "dusk"
	// [12,] "2025-04-24 05:10:43" "nauticalDawn"
	// [13,] "2025-04-24 21:27:44" "nauticalDusk"
	// [14,] "2025-04-24 04:28:19" "nightEnd"
	// [15,] "2025-04-24 22:10:08" "night"
	// [16,] "2025-04-24 07:01:14" "goldenHourEnd"
	// [17,] "2025-04-24 19:37:13" "goldenHour"
	want := SolarEvents{}
	w := func(t, e string) {
		ts, err := time.ParseInLocation(time.RFC3339, t, time.Local)
		if err != nil {
			fmt.Printf("Couldn't parse time '%s': %s\n", t, err.Error())
		}
		n := suncalc.DayTimeName(e)
		want[n] = suncalc.DayTime{Name: n, Value: ts}
	}
	w("2025-04-24T13:19:14-03:00", "solarNoon")
	w("2025-04-24T01:19:14-03:00", "nadir")
	w("2025-04-24T06:21:02-03:00", "sunrise")
	w("2025-04-24T20:17:25-03:00", "sunset")
	w("2025-04-24T06:24:14-03:00", "sunriseEnd")
	w("2025-04-24T20:14:13-03:00", "sunsetStart")
	w("2025-04-24T05:49:27-03:00", "dawn")
	w("2025-04-24T20:49:00-03:00", "dusk")
	w("2025-04-24T05:10:43-03:00", "nauticalDawn")
	w("2025-04-24T21:27:44-03:00", "nauticalDusk")
	w("2025-04-24T04:28:19-03:00", "nightEnd")
	w("2025-04-24T22:10:08-03:00", "night")
	w("2025-04-24T07:01:14-03:00", "goldenHourEnd")
	w("2025-04-24T19:37:13-03:00", "goldenHour")
	for i, v := range want {
		if ev[i].Value.Truncate(time.Second) != v.Value.Truncate(time.Second) {
			t.Errorf(`ShiftWrap.GetSolarEvents()[%s] = %q, want %q`, i, ev[i], v)
		}
	}
	SW.Quit()
}

func TestServiceIsRunning(t *testing.T) {
	// this package is only useful if the system is running systemd,
	// and that almost always means running app.init in the user context,
	// for the following Service, IsRunning() should return true
	s := Service{Name: "app.slice", IsSystemd: true}
	if !s.IsRunning() {
		t.Errorf(`IsRunning for service %v is false, want true`, s)
	}
	s = Service{Name: "totaly_fake_not_real"}
	if s.IsRunning() {
		t.Errorf(`IsRunning for service %v is true, want =false`, s)
	}
	s = Service{Name: "", running: false}
	if s.IsRunning() {
		t.Errorf(`IsRunning for service %v is true, want =false`, s)
	}
	s = Service{Name: "", running: true}
	if !s.IsRunning() {
		t.Errorf(`IsRunning for service %v is false, want true`, s)
	}
}

func TestServiceStartStop(t *testing.T) {
	s := Service{Name: "not-a-real-service", IsSystemd: true}
	sh := Shift{service: &s, Label: "foo", Setup: "ls /etc > /dev/null", Takedown: "echo yes"}
	// not sure how to make systemctl not prompt for root password here!
	if SW.Start(&s, &sh, time.Now()) == true {
		t.Errorf(`Start for bogus service should not have worked`)
	}
}

func TestServiceRunningPeriods(t *testing.T) {
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	// scr := SW.ServiceRunningPeriods(TestService, T1, true)
	//	fmt.Printf("Raw:\n%s\n", scr.String())
	sc := SW.ServiceRunningPeriods(TestService, T1, false)
	//	fmt.Printf("Cooked:\n%s\n", sc.String())
	want := []ShiftChange{
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 23, 20, 26, 9, 499362560, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 6, 6, 2, 686210816, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mid-day"],
			At:    time.Date(2025, time.April, 24, 12, 19, 14, 52065536, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["afternoon"],
			At:    time.Date(2025, time.April, 24, 16, 30, 0, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 20, 27, 25, 617919744, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 25, 6, 4, 27, 67336960, time.Local),
			Type:  Stop,
		},
	}
	if len(sc) != len(want) {
		t.Errorf(`TestServiceRunningPeriods returned %d shift changes but want %d`, len(sc), len(want))
	}
	for i, v := range want {
		if !sc[i].Equals(v) {
			t.Errorf(`TestServiceRunningPeriods returned item[%d] = %#v, want %#v`, i, sc[i], v)
		}
	}
	SW.Quit()
}

func TestServiceRunningPeriods_wonky(t *testing.T) {
	// test running periods where it is important to have a MustExclude item.
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	TestService3.Shifts = TestShifts4
	SW.AddService(TestService3)
	// On date T1, the Stop time is before the Start time, but this is not meant to
	// be an overnight shift, so midnight is excluded, and the running period is eliminated.
	sc := SW.ServiceRunningPeriods(TestService3, T1, false)
	want := []ShiftChange{}
	if len(sc) != len(want) {
		t.Errorf(`TestServiceRunningPeriods_wonky returned %d shift changes but want %d`, len(sc), len(want))
	}
	// On date T1 + 1, Start is shortly before Stop, so there is a running period
	SW.DropService("whammo")
	TestService3.Shifts = TestShifts4
	SW.AddService(TestService3)
	sc = SW.ServiceRunningPeriods(TestService3, T1.AddDate(0, 0, 1), false)
	want = []ShiftChange{
		ShiftChange{
			shift: TestShiftNamed["morning"],
			At:    time.Date(2025, time.April, 25, 10, 17, 45, 315431424, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["morning"],
			At:    time.Date(2025, time.April, 25, 10, 19, 4, 283621120, time.Local),
			Type:  Stop,
		},
	}
	if len(sc) != len(want) {
		t.Errorf(`TestServiceRunningPeriods_wonky returned %d shift changes but want %d`, len(sc), len(want))
	}
	for i := 0; i < min(len(sc), len(want)); i++ {
		if !want[i].Equals(sc[i]) {
			t.Errorf(`TestServiceRunningPeriods_wonky returned item[%d] = %#v, want %#v`, i, sc[i], want[i])
		}
	}
	SW.Quit()
}

func TestServiceRunningPeriods_raw(t *testing.T) {
	TestService.Shifts = TestShifts
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	sc := SW.ServiceRunningPeriods(TestService, T1, true)
	fmt.Printf("%v\n", sc)
	want := []ShiftChange{
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 23, 20, 26, 9, 499362560, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["aftertoday"],
			At:    time.Date(2025, time.April, 24, 3, 19, 24, 338511104, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["aftertoday"],
			At:    time.Date(2025, time.April, 24, 5, 19, 24, 338511104, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 6, 6, 2, 686210816, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mid-day"],
			At:    time.Date(2025, time.April, 24, 12, 19, 14, 52065536, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["afternoon"],
			At:    time.Date(2025, time.April, 24, 15, 0, 0, 0, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["mid-day"],
			At:    time.Date(2025, time.April, 24, 16, 19, 14, 52065536, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["afternoon"],
			At:    time.Date(2025, time.April, 24, 16, 30, 0, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mini1"],
			At:    time.Date(2025, time.April, 24, 17, 2, 0, 0, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["mini2"],
			At:    time.Date(2025, time.April, 24, 17, 2, 30, 0, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["mini1"],
			At:    time.Date(2025, time.April, 24, 17, 3, 0, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mini2"],
			At:    time.Date(2025, time.April, 24, 17, 3, 30, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 20, 27, 25, 617919744, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["beforetoday"],
			At:    time.Date(2025, time.April, 24, 21, 19, 04, 283621120, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["beforetoday"],
			At:    time.Date(2025, time.April, 24, 23, 19, 04, 283621120, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 25, 6, 4, 27, 67336960, time.Local),
			Type:  Stop,
		},
	}
	if len(sc) != len(want) {
		t.Errorf(`GetShiftChanges returned %d shift changes but want %d`, len(sc), len(want))
	}
	for i, v := range want {
		if !sc[i].Equals(v) {
			t.Errorf(`GetShiftChanges returned item[%d] = %#v, want %#v`, i, sc[i], v)
		}
	}
	SW.Quit()
}

func TestMergeOverlaps(t *testing.T) {
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	sc := SW.ServiceRunningPeriods(TestService, T1, true)
	sc = SW.MergeOverlaps(sc)
	want := []ShiftChange{
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 23, 20, 26, 9, 499362560, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 6, 6, 2, 686210816, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mid-day"],
			At:    time.Date(2025, time.April, 24, 12, 19, 14, 52065536, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["afternoon"],
			At:    time.Date(2025, time.April, 24, 16, 30, 0, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mini1"],
			At:    time.Date(2025, time.April, 24, 17, 2, 0, 0, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["mini2"],
			At:    time.Date(2025, time.April, 24, 17, 3, 30, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 20, 27, 25, 617919744, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 25, 6, 4, 27, 67336960, time.Local),
			Type:  Stop,
		},
	}
	if len(sc) != len(want) {
		t.Errorf(`MergeOverlaps returned %d shift changes but want %d`, len(sc), len(want))
	}
	for i, v := range want {
		if !sc[i].Equals(v) {
			t.Errorf(`MergeOverlaps returned item[%d] = %#v, want %#v`, i, sc[i], v)
		}
	}
	SW.Quit()
}

func TestGetCookedShiftChanges(t *testing.T) {
	SW = NewShiftWrap()
	SW.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	sc := SW.ServiceRunningPeriods(TestService, T1, false)
	want := []ShiftChange{
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 23, 20, 26, 9, 499362560, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 6, 6, 2, 686210816, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["mid-day"],
			At:    time.Date(2025, time.April, 24, 12, 19, 14, 52065536, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["afternoon"],
			At:    time.Date(2025, time.April, 24, 16, 30, 0, 0, time.Local),
			Type:  Stop,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 24, 20, 27, 25, 617919744, time.Local),
			Type:  Start,
		},
		ShiftChange{
			shift: TestShiftNamed["overnight"],
			At:    time.Date(2025, time.April, 25, 6, 4, 27, 67336960, time.Local),
			Type:  Stop,
		},
	}
	if len(sc) != len(want) {
		t.Errorf(`MergeOverlaps returned %d shift changes but want %d`, len(sc), len(want))
	}
	for i, v := range want {
		if !sc[i].Equals(v) {
			t.Errorf(`MergeOverlaps returned item[%d] = %#v, want %#v`, i, sc[i], v)
		}
	}
	SW.Quit()
}

func TestNewShiftWrapWithAClock(t *testing.T) {
	T2 := time.Date(2025, time.April, 23, 20, 26, 9, 299362560, time.Local)
	// run at warped time:  1 s = 2 h
	clock := timewarper.GetWarpedClock(7200, T2)
	clock.(*timewarper.Clock).SetUnsafe(true)
	sw2 := NewShiftWrapWithAClock(clock)
	done := validateShiftChangesUntil(t, sw2, sw2.NewShiftChangeListener(), T2.Add(40*time.Hour))
	sw2.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	sw2.AddService(TestService)
	sw2.ManageService(TestService, true)
	<-done
	sw2.ManageService(TestService, false)
	sw2.Quit()
}

func ExampleNewShiftWrapWithAClock_hurry() {
	T2 := time.Date(2025, time.April, 23, 20, 26, 8, 299362560, time.Local)
	clock := timewarper.GetWarpedClock(3600, T2)
	clock.(*timewarper.Clock).SetUnsafe(true)
	sw2 := NewShiftWrapWithAClock(clock)
	sw2.Hurry = true
	// run for a week
	weekDone := reportShiftChangesUntil(sw2.NewShiftChangeListener(), T2.Add(7*24*time.Hour))
	sw2.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	TestService.running = false
	sw2.AddService(TestService)
	sw2.ManageService(TestService, true)
	<-weekDone
	sw2.ManageService(TestService, false)
	sw2.Quit()
	// Output:
	// 2025-04-24 06:06:02 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-24 12:19:14 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-24 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-24 20:27:25 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-25 06:04:27 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-25 12:19:04 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-25 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-25 20:28:41 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-26 06:02:52 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-26 12:18:55 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-26 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-26 20:29:57 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-27 06:01:19 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-27 12:18:46 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-27 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-27 20:31:13 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-28 05:59:46 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-28 12:18:38 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-28 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-28 20:32:29 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-29 05:58:15 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-29 12:18:30 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-29 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-29 20:33:45 (bogus): Start shift overnight at sunset+10m0.2s
	// 2025-04-30 05:56:46 (bogus): Stop  shift overnight at sunrise-15m
	// 2025-04-30 12:18:23 (bogus): Start shift mid-day at solarNoon-1h
	// 2025-04-30 16:30:00 (bogus): Stop  shift afternoon at 16:30:00
	// 2025-04-30 20:35:00 (bogus): Start shift overnight at sunset+10m0.2s
}

func reportShiftChangesUntil(c chan ShiftChange, until time.Time) (rv chan bool) {
	rv = make(chan bool)
	go func(c chan ShiftChange, done chan bool) {
		<-c // discard first event, whose initial time isn't part of the test
		for sc := range c {
			fmt.Printf("%s (%s): %s\n", sc.trueAt.Format(time.DateTime), sc.Service().Name, sc.String())
			if !sc.At.Before(until) {
				done <- true
				return
			}
		}
	}(c, rv)
	return
}

func validateShiftChangesUntil(t *testing.T, sw *ShiftWrap, c chan ShiftChange, until time.Time) (rv chan bool) {
	rv = make(chan bool)
	go func(c chan ShiftChange, done chan bool) {
		timeTol := time.Second
		for sc := range c {
			delta := sc.At.Sub(sc.trueAt)
			sn := sc.Service().Name
			if delta > timeTol || delta < -timeTol {
				msg := fmt.Sprintf(`*** BADTOL: %s; shiftchange %s predicted for %s, triggered at %s`, delta, sn, sc.At.Format(time.DateTime), sc.trueAt.Format(time.DateTime))
				if t != nil {
					t.Error(msg)
				} else {
					fmt.Print(msg)
				}
			}
			if !sc.At.Before(until) {
				done <- true
				return
			}
		}
	}(c, rv)
	return
}

func TestNewShiftWrapWithAClock_two(t *testing.T) {
	T2 := time.Date(2025, time.April, 23, 20, 26, 9, 299362560, time.Local)
	clock := timewarper.GetWarpedClock(7200, T2)
	clock.(*timewarper.Clock).SetUnsafe(true)
	sw2 := NewShiftWrapWithAClock(clock)
	// run for 2 days
	weekDone := validateShiftChangesUntil(t, sw2, sw2.NewShiftChangeListener(), T2.Add(2*24*time.Hour))
	sw2.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	TestService2.Shifts = TestShifts2
	TestService.running = false
	TestService2.running = false
	sw2.AddService(TestService2)
	sw2.AddService(TestService)
	sw2.ManageService(TestService2, true)
	sw2.ManageService(TestService, true)
	<-weekDone
	sw2.ManageService(TestService2, false)
	sw2.ManageService(TestService, false)
	sw2.Quit()
}

func TestNewShiftWrapWithAClock_two_hurry(t *testing.T) {
	T2 := time.Date(2025, time.April, 23, 20, 26, 9, 299362560, time.Local)
	clock := timewarper.GetWarpedClock(3600, T2)
	clock.(*timewarper.Clock).SetUnsafe(true)
	sw2 := NewShiftWrapWithAClock(clock)
	sw2.Hurry = true
	// run for 7 days
	weekDone := validateShiftChangesUntil(t, sw2, sw2.NewShiftChangeListener(), T2.Add(7*24*time.Hour))
	sw2.Conf.Observer = Obs
	TestService.Shifts = TestShifts
	TestService2.Shifts = TestShifts2
	TestService.running = false
	TestService2.running = false
	sw2.AddService(TestService2)
	sw2.AddService(TestService)
	sw2.ManageService(TestService2, true)
	sw2.ManageService(TestService, true)
	<-weekDone
	sw2.ManageService(TestService2, false)
	sw2.ManageService(TestService, false)
	sw2.Quit()
}
