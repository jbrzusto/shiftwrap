package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jbrzusto/shiftwrap/v2"
	"github.com/jbrzusto/timewarper"
)

const (
	DefaultConfDir       = "/etc/shiftwrap"
	ConfDirEnvVar        = "SHIFTWRAP_DIR"
	DefaultHTTPInterface = ""
	DefaultHTTPPort      = 0x7AC0
)

var (
	confDir             string
	serviceConfDir      string
	SW                  *shiftwrap.ShiftWrap
	clockDilationFactor float64
	clockEpochString    string
)

func Usage() {
	fmt.Print(`
Usage:

  shiftwrapd [FLAGS]

Run the shiftwrap daemon.

FLAGS:

`)
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {
	var httpAddr string
	flag.StringVar(&httpAddr, "httpaddr", DefaultHTTPInterface+":"+strconv.Itoa(DefaultHTTPPort), "address:port from which to operate HTTP server; disabled if empty")
	flag.Float64Var(&clockDilationFactor, "clockdil", 1, "clock dilation factor; 1 = normal clock speed; 2 = double clock speed, etc.")
	flag.StringVar(&clockEpochString, "clockepoch", "", "clock epoch as YYYY-MM-DD HH:MM; default (\"\") means 'now'.  If neither -clockepoch nor -clockdil are specified, shiftwrapd uses the standard system clock")
	confDir = os.Getenv(ConfDirEnvVar)
	if confDir == "" {
		confDir = DefaultConfDir
	}
	serviceConfDir = path.Join(confDir, "services")
	flag.Parse()
	var cl timewarper.AClock
	if clockEpochString != "" {
		if cle, err := time.ParseInLocation(time.DateTime, clockEpochString, time.Local); err != nil {
			log.Fatalf("unable to parse clockepoch '%s': %s", clockEpochString, err.Error())
		} else {
			cl = timewarper.GetWarpedClock(clockDilationFactor, cle)
		}
	} else if clockDilationFactor != 1.0 {
		cl = timewarper.GetWarpedClock(clockDilationFactor, time.Now())
	} else {
		cl = timewarper.GetStandardClock()
	}
	cl.SetUnsafe(true)
	SW = shiftwrap.NewShiftWrapWithAClock(cl)
	SW.ReadConfig(confDir)
	SW.ReadConfig(serviceConfDir)
	http.HandleFunc("/config", HandleConfig)
	http.HandleFunc("/time", HandleTime)
	http.HandleFunc("/timer", HandleTimer)
	http.HandleFunc("/queue", HandleQueue)
	http.HandleFunc("/services", HandleServices)
	http.HandleFunc("/service/{sn}", HandleService)
	http.HandleFunc("/service/{sn}/shiftchanges", HandleShiftChanges)
	http.HandleFunc("/service/{sn}/shiftchanges/{date}/raw", HandleShiftChangesDateRaw)
	http.HandleFunc("/service/{sn}/shiftchanges/{date}", HandleShiftChangesDate)
	http.ListenAndServe(httpAddr, nil)
}

func HTErr(w http.ResponseWriter, msg string, v ...any) {
	HTErrStatus(http.StatusBadRequest, w, msg, v...)
}

func HTErrStatus(status int, w http.ResponseWriter, msg string, v ...any) {
	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf(msg, v...)))
	w.Write([]byte("\r\n"))
}

func HTNotImpl(w http.ResponseWriter, fun string) {
	HTErrStatus(http.StatusNotImplemented, w, "%s not implemented", fun)
}

var OKResp = []byte("{\"status\":\"ok\"}")

func HTOkay(w http.ResponseWriter) {
	w.Write(OKResp)
}

func HandleTime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, _ := json.Marshal(SW.Clock.Now().Format(time.RFC3339Nano))
		SendResponse(b, w)
	case http.MethodPut:
		fallthrough
	default:
		HTErr(w, "not implemented")
	}
}

func HandleTimer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, _ := json.Marshal(SW.GetTimer().Format(time.RFC3339Nano))
		SendResponse(b, w)
	case http.MethodPut:
		fallthrough
	default:
		HTErr(w, "not implemented")
	}
}

func HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, _ := json.Marshal(SW.Conf)
		SendResponse(b, w)
	case http.MethodPut:
		fallthrough
	default:
		HTErr(w, "not implemented")
	}
}

func HandleQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, _ := json.Marshal(SW.GetQueue())
		SendResponse(b, w)
	case http.MethodPut:
		fallthrough
	default:
		HTErr(w, "not implemented")
	}
}

func HandleServices(w http.ResponseWriter, r *http.Request) {
	b, _ := json.Marshal(SW.GetServiceNames())
	SendResponse(b, w)
}

// lookupService looks up a service by name, instantiating it
// if it is a previously-unknown instance of a template.
// If it can't do this, it returns nil and if mustExist is true, writes an appropriate
// error to w.
func lookupService(w http.ResponseWriter, sn string, mustExist bool) (rv *shiftwrap.Service) {
	if rv = SW.ServiceByName(sn, false); rv == nil && mustExist {
		before, _, found := strings.Cut(sn, "@")
		if found {
			HTErrStatus(http.StatusNotFound, w, "service template %s@ not known", before)
		} else {
			HTErrStatus(http.StatusNotFound, w, "service %s not known", sn)
		}
	}
	return
}

// HandleService returns, creates, modifies, or deletes the named service
func HandleService(w http.ResponseWriter, r *http.Request) {
	sn := r.PathValue("sn")
	s := lookupService(w, sn, r.Method != http.MethodPut)
	switch r.Method {
	case http.MethodGet:
		if s == nil {
			return
		}
		b, _ := json.Marshal(s)
		SendResponse(b, w)
	case http.MethodDelete:
		if s == nil {
			return
		}
		SW.DropService(sn)
		HTOkay(w)
	case http.MethodPut:
		// create service in case it doesn't already exist
		if s == nil {
			s = SW.ServiceByName(sn, true)
			if s == nil {
				HTErr(w, "unknown service: %s", sn)
				return
			}
		}
		setManaged := false
		willManage := false
		if len(r.Header["Content-Type"]) > 0 {
			errmsg := ""
			ctype := r.Header["Content-Type"][0]
			if ctype != "application/json" {
				HTErr(w, "invalid Content-Type: %s", ctype)
				return
			}
			var (
				buf []byte
				err error
			)
			buf, err = io.ReadAll(r.Body)
			if err != nil {
				HTErr(w, "bad or missing settings payload: %s", err.Error())
				return
			}
			tmps := map[string]any{}
			if err = json.Unmarshal(buf, &tmps); err != nil {
				HTErr(w, "unable to parse JSON service payload: %s", err.Error())
				return
			}
			if v, have := tmps["IsManaged"]; have {
				if vb, ok := v.(bool); ok {
					setManaged = true
					willManage = vb
				} else {
					errmsg += "IsManaged must be a boolean; "
				}
				delete(tmps, "IsManaged")
			}
			if v, have := tmps["IsSystemd"]; have {
				if vb, ok := v.(bool); ok {
					s.IsSystemd = vb
				} else {
					errmsg += "IsSystemd must be a boolean; "
				}
				delete(tmps, "IsSystemd")
			}
			if v, have := tmps["MinRuntime"]; have {
				if vs, ok := v.(string); ok {
					if mr, err := time.ParseDuration(vs); err == nil {
						s.MinRuntime = shiftwrap.TidyDuration(mr)
					} else {
						errmsg += "MinRuntime failed to parse as a time.Duration: " + err.Error()
					}
				} else {
					errmsg += "MinRuntime must be a string representing a time.Duration; "
				}
				delete(tmps, "MinRuntime")
			}
			if v, have := tmps["Name"]; have {
				if vs, ok := v.(string); ok {
					s.Name = vs
					if s.Name != sn {
						log.Printf("doing add+drop service")
						SW.DropService(sn)
						SW.AddService(s)
					}
				} else {
					errmsg += "Name must be a string; "
				}
				delete(tmps, "Name")
			}
			if v, have := tmps["Shifts"]; have {
				if vs, ok := v.(map[string]any); ok {
					if len(vs) > 0 {
						for n, sh := range vs {
							if shm, ok := sh.(map[string]any); !ok {
								errmsg += "Shift " + n + " must be an object; "
							} else {
								tmpsh := &shiftwrap.Shift{}
								if lab, ok := shm["Label"].(string); ok && lab == "" {
									errmsg += "Label for shift " + n + " must be a non-empty string; "
								} else {
									if ok {
										tmpsh.Label = lab
									} else {
										tmpsh.Label = n
									}
								}
								if start, ok := shm["Start"].(string); start == "" || !ok {
									errmsg += "Start for shift " + n + " must be a string; "
								} else {
									if sts, err := shiftwrap.ParseTimeSpec(start); err != nil {
										errmsg += "Start for shift " + n + "failed to parse as a timespec: " + err.Error()
									} else {
										tmpsh.Start = &sts
									}
								}
								if stop, ok := shm["Stop"].(string); stop == "" || !ok {
									errmsg += "Stop for shift " + n + " must be a string; "
								} else {
									if sts, err := shiftwrap.ParseTimeSpec(stop); err != nil {
										errmsg += "Stop for shift " + n + "failed to parse as a timespec: " + err.Error()
									} else {
										tmpsh.Stop = &sts
									}
								}
								if v, have := shm["StopBeforeStart"]; have {
									if sbs, ok := v.(bool); !ok {
										errmsg += "StopBeforeStart for shift " + n + " must be a bool; "
									} else {
										tmpsh.StopBeforeStart = sbs
									}
								}
								if v, have := shm["Setup"]; have {
									if setup, ok := v.(string); !ok {
										errmsg += "Setup for shift " + n + " must be a string; "
									} else {
										tmpsh.Setup = setup
									}
								}
								if v, have := shm["Takedown"]; have {
									if takedown, ok := v.(string); !ok {
										errmsg += "Takedown for shift " + n + " must be a string; "
									} else {
										tmpsh.Takedown = takedown
									}
								}
								if errmsg == "" {
									SW.AddShifts(s, tmpsh)
								}
							}
						}
					}
				} else {
					errmsg += "Shifts must be an array of named shifts; "
				}
				delete(tmps, "Shifts")
			}
			if len(tmps) > 0 {
				errmsg += "unknown fields:"
				for k := range tmps {
					errmsg += " " + k
				}
			}
			if errmsg != "" {
				HTErr(w, "errors in PUT Service: `%s`", errmsg)
				return
			} else if setManaged {
				SW.ManageService(s, willManage)
			}
		}
		if err := SW.WriteConfig(s, serviceConfDir); err != nil {
			log.Printf("error (re-)writing config for service %s: %s", s.Name, err.Error())
		}
		HTOkay(w)
	default:
		HTNotImpl(w, r.Method+r.URL.Path)
	}
}

type ShiftChangeRequestType int

func HandleShiftChangesDateRaw(w http.ResponseWriter, r *http.Request) {
	doHandleShiftChanges(w, r, true, true)
}

func HandleShiftChangesDate(w http.ResponseWriter, r *http.Request) {
	doHandleShiftChanges(w, r, true, false)
}

func HandleShiftChanges(w http.ResponseWriter, r *http.Request) {
	doHandleShiftChanges(w, r, false, false)
}

func doHandleShiftChanges(w http.ResponseWriter, r *http.Request, withdate bool, raw bool) {
	sn := r.PathValue("sn")
	s := lookupService(w, sn, true)
	if s == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if withdate {
			dateString := r.PathValue("date")
			date, err := time.ParseInLocation(time.DateOnly, dateString, time.Local)
			if err != nil {
				HTErr(w, "can't parse date %s: %s", dateString, err.Error())
				return
			}
			date = date.Add(12 * time.Hour)
			var b []byte
			b, err = json.Marshal(SW.ServiceShiftChanges(s, date, raw))
			SendResponse(b, w)
		} else {
			b, _ := json.Marshal(s.GetCurrentShiftChanges())
			SendResponse(b, w)
		}
	default:
		HTNotImpl(w, r.Method+r.URL.Path)
	}
}

func SendResponse(b []byte, w http.ResponseWriter) {
	h := w.Header()
	h.Set("content-type", "application/json")
	w.Write(b)
}
