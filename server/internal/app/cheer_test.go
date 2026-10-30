package app

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// ledSession is the session of node's LED controller in a cheerHarness.
func ledSession(node NodeID) SessionID { return SessionID("led" + node.wireName()) }

// cheerHarness connects the players, with those of manual eligible in the next
// game, and every LED controller.
func cheerHarness(t *testing.T, options Options, manual, automatic []NodeID) *harness {
	t.Helper()
	h := topPercentHarness(t, options, manual, automatic)
	for _, node := range ledNodes {
		h.connectLED(ledSession(node), ledUID(node), node)
	}
	return h
}

// cheersSent is the CHEER lines sent to each LED controller, sorted, by the
// controller's node.
func (h *harness) cheersSent() map[string][]string {
	h.t.Helper()
	sent := make(map[string][]string)
	for _, node := range ledNodes {
		for _, line := range h.lines(ledSession(node)) {
			if strings.HasPrefix(line, "CHEER ") {
				sent[node.wireName()] = append(sent[node.wireName()], line)
			}
		}
	}
	for _, lines := range sent {
		slices.Sort(lines)
	}
	return sent
}

// The controllers and links follow the layout table of led.md.
func TestCheersGoToEveryLinkOfTheBest(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		eligible, ineligible map[NodeID]int
		want                 map[string][]string
	}{
		{"P1", map[NodeID]int{1: 100}, nil, map[string][]string{
			"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "B1": {"CHEER P1 P1 game"}, "B3": {"CHEER P1 P1 game"},
		}},
		{"P2", map[NodeID]int{2: 100}, nil, map[string][]string{
			"P1": {"CHEER P2 P2 game"}, "P3": {"CHEER P2 P2 game"}, "B1": {"CHEER P2 P2 game"}, "B2": {"CHEER P2 P2 game"},
		}},
		{"P3", map[NodeID]int{3: 100}, nil, map[string][]string{
			"P3": {"CHEER P2 P3 game", "CHEER P4 P3 game"}, "B2": {"CHEER P3 P3 game"}, "B3": {"CHEER P3 P3 game"},
		}},
		{"P4", map[NodeID]int{4: 100}, nil, map[string][]string{
			"P3": {"CHEER P4 P4 game"}, "P5": {"CHEER P4 P4 game"}, "B1": {"CHEER P4 P4 game"}, "B2": {"CHEER P4 P4 game"},
		}},
		{"P5", map[NodeID]int{5: 100}, nil, map[string][]string{
			"P5": {"CHEER P4 P5 game", "CHEER P6 P5 game"}, "B1": {"CHEER P5 P5 game"}, "B3": {"CHEER P5 P5 game"},
		}},
		{"P6", map[NodeID]int{6: 100}, nil, map[string][]string{
			"P5": {"CHEER P6 P6 game"}, "P7": {"CHEER P6 P6 game"}, "B1": {"CHEER P6 P6 game"}, "B2": {"CHEER P6 P6 game"},
		}},
		{"P7", map[NodeID]int{7: 100}, nil, map[string][]string{
			"P7": {"CHEER P6 P7 game", "CHEER P8 P7 game"}, "B1": {"CHEER P7 P7 game"}, "B3": {"CHEER P7 P7 game"},
		}},
		{"P8", map[NodeID]int{8: 100}, nil, map[string][]string{
			"P1": {"CHEER P8 P8 game"}, "P7": {"CHEER P8 P8 game"}, "B2": {"CHEER P8 P8 game"}, "B3": {"CHEER P8 P8 game"},
		}},
		// P3 is above them but not eligible.
		{"ties apart", map[NodeID]int{1: 500, 2: 300, 4: 500}, map[NodeID]int{3: 900}, map[string][]string{
			"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "P3": {"CHEER P4 P4 game"}, "P5": {"CHEER P4 P4 game"},
			"B1": {"CHEER P1 P1 game", "CHEER P4 P4 game"}, "B2": {"CHEER P4 P4 game"}, "B3": {"CHEER P1 P1 game"},
		}},
		// The link between tied neighbors gets one CHEER, from the lower node.
		{"tied neighbors", map[NodeID]int{1: 500, 2: 500}, nil, map[string][]string{
			"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "P3": {"CHEER P2 P2 game"},
			"B1": {"CHEER P1 P1 game", "CHEER P2 P2 game"}, "B2": {"CHEER P2 P2 game"}, "B3": {"CHEER P1 P1 game"},
		}},
		{"tied neighbors P8 and P1", map[NodeID]int{1: 500, 8: 500}, nil, map[string][]string{
			"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "P7": {"CHEER P8 P8 game"},
			"B1": {"CHEER P1 P1 game"}, "B2": {"CHEER P8 P8 game"}, "B3": {"CHEER P1 P1 game", "CHEER P8 P8 game"},
		}},
		{"no eligible player", nil, map[NodeID]int{1: 300}, map[string][]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cheerHarness(t, Options{}, slices.Sorted(maps.Keys(tc.eligible)), slices.Sorted(maps.Keys(tc.ineligible)))
			scores := maps.Clone(tc.eligible)
			if scores == nil {
				scores = make(map[NodeID]int)
			}
			maps.Copy(scores, tc.ineligible)
			h.finishManually(scores)
			if got := h.cheersSent(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("CHEER lines\n got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// p1Cheers is what the LED controllers get when P1 alone has the best.
func p1Cheers(style string) map[string][]string {
	return map[string][]string{
		"P1": {"CHEER P2 P1 " + style, "CHEER P8 P1 " + style}, "B1": {"CHEER P1 P1 " + style}, "B3": {"CHEER P1 P1 " + style},
	}
}

func TestCheerStyleIsTheHighestBestSet(t *testing.T) {
	// The all-time best of 800 is from another hour; this hour's is 400.
	history := []GameRecord{
		testRecord(jst(12, 0, 0, 0), FinishTimeout, map[NodeID]int{5: 800}, nil),
		testRecord(epochHour(0), FinishTimeout, map[NodeID]int{5: 400}, nil),
	}
	for _, tc := range []struct {
		name    string
		options Options
		manual  bool // Finished by the admin rather than the game time.
		score   int
		want    string
	}{
		{"a new all-time best", Options{Records: history}, false, 900, "record"},
		{"a new best of the hour only", Options{Records: history}, false, 600, "hour"},
		// A tie leaves the all-time best with the earlier game.
		{"a tie with the all-time best", Options{Records: history}, false, 800, "hour"},
		{"a tie with the best of the hour", Options{Records: history}, false, 400, "game"},
		// The game would set both bests if it had run its full time.
		{"a manual finish", Options{Records: history}, true, 900, "game"},
		// The Snapshot counts it as both bests, but the summary does not.
		{"unavailable records", Options{Records: history, RecordsUnavailable: "file is not a database"}, false, 900, "game"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cheerHarness(t, tc.options, []NodeID{1, 2}, nil)
			scores := map[NodeID]int{1: tc.score, 2: 100}
			if tc.manual {
				h.finishManually(scores)
			} else {
				h.playToTimeout(scores)
			}
			if got, want := h.cheersSent(), p1Cheers(tc.want); !reflect.DeepEqual(got, want) {
				t.Errorf("CHEER lines\n got %v\nwant %v", got, want)
			}
		})
	}
}

func TestTiedPlayersCheerInTheSameStyle(t *testing.T) {
	h := cheerHarness(t, Options{}, []NodeID{1, 2}, nil)
	h.playToTimeout(map[NodeID]int{1: 900, 2: 900})
	want := map[string][]string{
		"P1": {"CHEER P2 P1 record", "CHEER P8 P1 record"}, "P3": {"CHEER P2 P2 record"},
		"B1": {"CHEER P1 P1 record", "CHEER P2 P2 record"}, "B2": {"CHEER P2 P2 record"}, "B3": {"CHEER P1 P1 record"},
	}
	if got := h.cheersSent(); !reflect.DeepEqual(got, want) {
		t.Errorf("CHEER lines\n got %v\nwant %v", got, want)
	}
}

func TestCheersSkipControllersThatCannotShowThem(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(h *harness) // Before the game.
		playing func(h *harness) // Just before Finish.
		want    map[string][]string
	}{
		{"disconnected", func(h *harness) { h.must(h.a.Disconnected(h.ctx, ledSession(9))) }, nil,
			map[string][]string{"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "B3": {"CHEER P1 P1 game"}}},
		{"unassigned", func(h *harness) {
			h.execute(Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: ledUID(11)})
			h.message(ledSession(11), IncomingMessage{Type: "hello", Kind: LEDDevice, UID: ledUID(11)})
		}, nil, map[string][]string{"P1": {"CHEER P2 P1 game", "CHEER P8 P1 game"}, "B1": {"CHEER P1 P1 game"}}},
		// A preview cannot start while Playing, and Start ends one, so the
		// test sets it directly.
		{"previewing", nil, func(h *harness) { h.a.preview = &preview{uid: ledUID(1), node: 1} },
			map[string][]string{"B1": {"CHEER P1 P1 game"}, "B3": {"CHEER P1 P1 game"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cheerHarness(t, Options{}, []NodeID{1}, nil)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			h.start()
			h.tick(30 * time.Second)
			h.do(func() {
				h.a.game.nodes[1].score = 100
				if tc.playing != nil {
					tc.playing(h)
				}
			})
			h.execute(Command{Operation: Finish})
			if got := h.cheersSent(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("CHEER lines\n got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// A failed CHEER closes only its controller's session; the others still get
// theirs and the result stands.
func TestCheerSendFailureClosesOnlyThatController(t *testing.T) {
	h := cheerHarness(t, Options{}, []NodeID{1}, nil)
	h.start()
	h.tick(30 * time.Second)
	h.do(func() { h.a.game.nodes[1].score = 100 })
	h.mu.Lock()
	h.failType = "CHEER"
	h.mu.Unlock()
	h.execute(Command{Operation: Finish})
	h.mu.Lock()
	closed := slices.Sorted(slices.Values(h.closed))
	h.mu.Unlock()
	if want := []SessionID{ledSession(9), ledSession(11), ledSession(1)}; !reflect.DeepEqual(closed, want) {
		t.Errorf("closed sessions %v, want %v", closed, want)
	}
	if s := h.state(); s.Phase != Finished || s.Records.GameBest == nil || s.Records.GameBest.Score != 100 {
		t.Errorf("the failure changed the result: phase %s, best %+v", s.Phase, s.Records.GameBest)
	}
	for _, node := range ledNodes {
		want := !slices.Contains(closed, ledSession(node))
		if d := h.device(ledUID(node)); d.Connected != want {
			t.Errorf("LED %s connected=%t, want %t", node.wireName(), d.Connected, want)
		}
	}
}

// An LED controller set up again while Finished does not get the CHEER
// commands again.
func TestCheersAreSentOnlyAtFinished(t *testing.T) {
	h := cheerHarness(t, Options{}, []NodeID{1}, nil)
	h.playToTimeout(map[NodeID]int{1: 900})
	if got, want := h.cheersSent(), p1Cheers("record"); !reflect.DeepEqual(got, want) {
		t.Fatalf("CHEER lines at Finished\n got %v\nwant %v", got, want)
	}
	h.clear()

	// B1 reconnects, P1 identifies itself again, B3 ends a preview, and P3 and
	// P5 swap boards.
	h.must(h.a.Disconnected(h.ctx, ledSession(9)))
	h.must(h.a.Connected(h.ctx, "ledB1b"))
	h.message("ledB1b", IncomingMessage{Type: "hello", Kind: LEDDevice, UID: ledUID(9)})
	h.message(ledSession(1), IncomingMessage{Type: "hello", Kind: LEDDevice, UID: ledUID(1)})
	h.execute(Command{Operation: SetDevicePreview, DeviceID: ledUID(11)})
	h.execute(Command{Operation: SetDevicePreview})
	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: ledUID(3), OtherDeviceID: ledUID(5)})
	h.tick(time.Duration(h.clock.Load()) + 10*time.Second)

	for _, m := range h.messages() {
		if m.message.Type == "CHEER" {
			t.Errorf("CHEER sent again to %s: %+v", m.session, m.message.Data)
		}
	}
	for _, tc := range []struct {
		session SessionID
		config  string
	}{
		{"ledB1b", "CONFIG B1 false"}, {ledSession(1), "CONFIG P1 false"}, {ledSession(11), "CONFIG B3 false"},
		{ledSession(3), "CONFIG P5 false"}, {ledSession(5), "CONFIG P3 false"},
	} {
		if !slices.Contains(h.lines(tc.session), tc.config) {
			t.Errorf("%s lines %v lack %q", tc.session, h.lines(tc.session), tc.config)
		}
	}
}

// Reset during the cheer sends only the usual commands, which stop it.
func TestResetAfterCheersSendsTheUsualClear(t *testing.T) {
	h := cheerHarness(t, Options{}, []NodeID{1}, nil)
	h.playToTimeout(map[NodeID]int{1: 900})
	h.clear()
	h.execute(Command{Operation: Reset})
	for _, node := range ledNodes {
		want := []string{"CLEAR"}
		if node.bot() {
			want = append(want, "BUFFER 0")
		}
		if got := h.lines(ledSession(node)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s Reset lines %v, want %v", node.wireName(), got, want)
		}
	}
}

func TestCheerLine(t *testing.T) {
	line, err := Message{Type: "CHEER", Data: CheerData{Neighbor: 1, Node: 1, Style: "record"}}.MarshalLine()
	if err != nil || string(line) != "CHEER P1 P1 record" {
		t.Fatalf("CHEER line %q, %v", line, err)
	}
	for _, d := range []CheerData{
		// No controller has a bot as a neighbor (led.md).
		{Neighbor: 0, Node: 1, Style: "game"}, {Neighbor: 9, Node: 1, Style: "game"},
		{Neighbor: 2, Node: 0, Style: "game"}, {Neighbor: 2, Node: 9, Style: "game"},
		{Neighbor: 2, Node: 1, Style: "Game"}, {Neighbor: 2, Node: 1, Style: ""},
	} {
		if _, err := (Message{Type: "CHEER", Data: d}).MarshalLine(); err == nil {
			t.Errorf("invalid CHEER accepted: %+v", d)
		}
	}
}
