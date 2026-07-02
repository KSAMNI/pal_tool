package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestValidateSchedules(t *testing.T) {
	valid := []scheduleRecord{
		{Time: "04:00", Action: "restart", Enabled: true},
		{Time: "04:00", Action: "stop", Enabled: true},
		{Time: "23:59", Action: "start", Enabled: false},
	}
	if err := validateSchedules(valid); err != nil {
		t.Fatalf("validateSchedules(valid) error = %v", err)
	}
	if err := validateSchedules(nil); err != nil {
		t.Fatalf("validateSchedules(nil) error = %v", err)
	}

	for _, tc := range []struct {
		name  string
		items []scheduleRecord
	}{
		{name: "hour_out_of_range", items: []scheduleRecord{{Time: "24:00", Action: "stop"}}},
		{name: "minute_out_of_range", items: []scheduleRecord{{Time: "04:60", Action: "stop"}}},
		{name: "missing_leading_zero", items: []scheduleRecord{{Time: "4:00", Action: "stop"}}},
		{name: "empty_time", items: []scheduleRecord{{Time: "", Action: "stop"}}},
		{name: "invalid_action", items: []scheduleRecord{{Time: "04:00", Action: "reboot"}}},
		{name: "duplicate_time_action", items: []scheduleRecord{
			{Time: "04:00", Action: "stop"},
			{Time: "04:00", Action: "stop"},
		}},
		{name: "too_many", items: makeTestSchedules(maxScheduledActions + 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSchedules(tc.items); err == nil {
				t.Fatalf("validateSchedules(%s) expected error", tc.name)
			}
		})
	}
}

func makeTestSchedules(count int) []scheduleRecord {
	items := make([]scheduleRecord, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, scheduleRecord{
			Time:    fmt.Sprintf("%02d:%02d", i/60, i%60),
			Action:  "restart",
			Enabled: true,
		})
	}
	return items
}

func TestScheduleDueWithin(t *testing.T) {
	location := time.FixedZone("test", 8*3600)
	base := time.Date(2026, 7, 1, 4, 0, 0, 0, location)
	for _, tc := range []struct {
		name string
		spec string
		from time.Time
		to   time.Time
		want bool
	}{
		{name: "inside_window", spec: "04:00", from: base.Add(-20 * time.Second), to: base.Add(10 * time.Second), want: true},
		{name: "window_end_inclusive", spec: "04:00", from: base.Add(-30 * time.Second), to: base, want: true},
		{name: "window_start_exclusive", spec: "04:00", from: base, to: base.Add(30 * time.Second), want: false},
		{name: "not_yet_due", spec: "04:01", from: base.Add(-30 * time.Second), to: base, want: false},
		{name: "already_past", spec: "03:59", from: base.Add(-30 * time.Second), to: base, want: false},
		{
			name: "crosses_midnight",
			spec: "00:00",
			from: time.Date(2026, 6, 30, 23, 59, 50, 0, location),
			to:   time.Date(2026, 7, 1, 0, 0, 20, 0, location),
			want: true,
		},
		{name: "invalid_spec", spec: "later", from: base.Add(-time.Minute), to: base.Add(time.Minute), want: false},
		{name: "empty_window", spec: "04:00", from: base.Add(10 * time.Second), to: base.Add(10 * time.Second), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduleDueWithin(tc.spec, tc.from, tc.to); got != tc.want {
				t.Fatalf("scheduleDueWithin(%q, %v, %v) = %v, want %v", tc.spec, tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestSchedulesAPIRoundTrip(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	server, client := newAuthenticatedTestServer(t, panel)

	payload := schedulesPayload{Schedules: []scheduleRecord{
		{Time: "23:00", Action: "stop", Enabled: false},
		{Time: "05:30", Action: "restart", Enabled: true},
	}}
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/schedules", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/schedules status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var saved schedulesPayload
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if len(saved.Schedules) != 2 {
		t.Fatalf("saved schedules = %d, want 2", len(saved.Schedules))
	}
	if saved.Schedules[0].Time != "05:30" || saved.Schedules[0].Action != "restart" || !saved.Schedules[0].Enabled {
		t.Fatalf("first saved schedule = %+v, want 05:30 restart enabled", saved.Schedules[0])
	}
	if saved.Schedules[1].Time != "23:00" || saved.Schedules[1].Enabled {
		t.Fatalf("second saved schedule = %+v, want 23:00 disabled", saved.Schedules[1])
	}
	if saved.Schedules[0].ID == 0 || saved.Schedules[1].ID == 0 {
		t.Fatalf("saved schedules missing ids: %+v", saved.Schedules)
	}

	respGet := doJSON(t, client, http.MethodGet, server.URL+"/api/schedules", nil)
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/schedules status = %d, want %d", respGet.StatusCode, http.StatusOK)
	}
	var listed schedulesPayload
	if err := json.NewDecoder(respGet.Body).Decode(&listed); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if len(listed.Schedules) != 2 || listed.Schedules[0].Time != "05:30" {
		t.Fatalf("listed schedules = %+v, want 2 entries sorted by time", listed.Schedules)
	}

	respBad := doJSON(t, client, http.MethodPut, server.URL+"/api/schedules", schedulesPayload{
		Schedules: []scheduleRecord{{Time: "99:00", Action: "restart", Enabled: true}},
	})
	defer respBad.Body.Close()
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid schedule status = %d, want %d", respBad.StatusCode, http.StatusBadRequest)
	}

	respEmpty := doJSON(t, client, http.MethodPut, server.URL+"/api/schedules", schedulesPayload{Schedules: []scheduleRecord{}})
	defer respEmpty.Body.Close()
	if respEmpty.StatusCode != http.StatusOK {
		t.Fatalf("PUT empty schedules status = %d, want %d", respEmpty.StatusCode, http.StatusOK)
	}
	respGetEmpty := doJSON(t, client, http.MethodGet, server.URL+"/api/schedules", nil)
	defer respGetEmpty.Body.Close()
	var cleared schedulesPayload
	if err := json.NewDecoder(respGetEmpty.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode cleared response: %v", err)
	}
	if len(cleared.Schedules) != 0 {
		t.Fatalf("cleared schedules = %+v, want empty", cleared.Schedules)
	}
}

func TestRunDueScheduledActionsStartsServerAndSkipsDisabled(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	var mu sync.Mutex
	started := 0
	stopped := 0
	panel.isServerRunningFunc = func() bool { return false }
	panel.serverProcessDetector = func(settingsPayload) (bool, error) { return false, nil }
	panel.startServerProcessFunc = func(settingsPayload) error {
		mu.Lock()
		started++
		mu.Unlock()
		return nil
	}
	panel.stopServerProcessFunc = func(time.Duration) error {
		mu.Lock()
		stopped++
		mu.Unlock()
		return nil
	}

	if _, err := panel.replaceSchedules([]scheduleRecord{
		{Time: "04:00", Action: "start", Enabled: true},
		{Time: "04:00", Action: "stop", Enabled: false},
	}); err != nil {
		t.Fatalf("replaceSchedules() error = %v", err)
	}

	from := time.Date(2026, 7, 1, 3, 59, 45, 0, time.Local)
	to := time.Date(2026, 7, 1, 4, 0, 15, 0, time.Local)
	panel.runDueScheduledActions(from, to)

	mu.Lock()
	defer mu.Unlock()
	if started != 1 {
		t.Fatalf("started = %d, want 1", started)
	}
	if stopped != 0 {
		t.Fatalf("stopped = %d, want 0 (schedule disabled)", stopped)
	}
	tasks, err := panel.listTasks(5)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1: %+v", len(tasks), tasks)
	}
	if tasks[0].Type != "scheduled_start" || tasks[0].Status != "success" {
		t.Fatalf("task = %s/%s, want scheduled_start/success", tasks[0].Type, tasks[0].Status)
	}
}

func TestExecuteScheduledRestartSavesStopsAndStarts(t *testing.T) {
	previousDelay := scheduledSaveSettleDelay
	scheduledSaveSettleDelay = 0
	t.Cleanup(func() { scheduledSaveSettleDelay = previousDelay })

	var mu sync.Mutex
	var order []string
	restServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, "save:"+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer restServer.Close()

	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "rest_api_url", restServer.URL)

	running := true
	panel.isServerRunningFunc = func() bool {
		mu.Lock()
		defer mu.Unlock()
		return running
	}
	panel.stopServerProcessFunc = func(time.Duration) error {
		mu.Lock()
		order = append(order, "stop")
		running = false
		mu.Unlock()
		return nil
	}
	panel.startServerProcessFunc = func(settingsPayload) error {
		mu.Lock()
		order = append(order, "start")
		mu.Unlock()
		return nil
	}

	panel.executeScheduledAction(scheduleRecord{Time: "04:00", Action: "restart", Enabled: true})

	mu.Lock()
	defer mu.Unlock()
	want := []string{"save:/save", "stop", "start"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	tasks, err := panel.listTasks(5)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != "scheduled_restart" || tasks[0].Status != "success" {
		t.Fatalf("tasks = %+v, want one successful scheduled_restart", tasks)
	}
}

func TestExecuteScheduledStopWithoutRunningServerIsNoOp(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	var mu sync.Mutex
	stopped := 0
	panel.isServerRunningFunc = func() bool { return false }
	panel.serverProcessDetector = func(settingsPayload) (bool, error) { return false, nil }
	panel.stopServerProcessFunc = func(time.Duration) error {
		mu.Lock()
		stopped++
		mu.Unlock()
		return nil
	}

	panel.executeScheduledAction(scheduleRecord{Time: "04:00", Action: "stop", Enabled: true})

	mu.Lock()
	defer mu.Unlock()
	if stopped != 0 {
		t.Fatalf("stopped = %d, want 0", stopped)
	}
	tasks, err := panel.listTasks(5)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != "scheduled_stop" || tasks[0].Status != "success" {
		t.Fatalf("tasks = %+v, want one successful scheduled_stop", tasks)
	}
}

func TestExecuteScheduledRestartFailsWithExternalServer(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()
	setTestAppSetting(t, panel, "pal_server_path", t.TempDir())

	var mu sync.Mutex
	started := 0
	panel.isServerRunningFunc = func() bool { return false }
	panel.serverProcessDetector = func(settingsPayload) (bool, error) { return true, nil }
	panel.startServerProcessFunc = func(settingsPayload) error {
		mu.Lock()
		started++
		mu.Unlock()
		return nil
	}

	panel.executeScheduledAction(scheduleRecord{Time: "04:00", Action: "restart", Enabled: true})

	mu.Lock()
	defer mu.Unlock()
	if started != 0 {
		t.Fatalf("started = %d, want 0", started)
	}
	tasks, err := panel.listTasks(5)
	if err != nil {
		t.Fatalf("listTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != "scheduled_restart" || tasks[0].Status != "failed" {
		t.Fatalf("tasks = %+v, want one failed scheduled_restart", tasks)
	}
}
